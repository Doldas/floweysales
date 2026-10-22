package server

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const flowHTTPMaxBytes = 64 * 1024

// flowPublicURL rejects local, private, metadata, and non-HTTP destinations.
// DNS addresses are validated again at connect time and pinned to the resolved IP.
func flowPublicURL(raw string) (*url.URL, error) {
	if len(raw) > 2048 {
		return nil, errors.New("URL exceeds limit")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("only http(s) is supported")
	}
	if u.User != nil || u.Fragment != "" || u.Hostname() == "" {
		return nil, errors.New("URL credentials or fragments are forbidden")
	}
	if port := u.Port(); port != "" && port != "80" && port != "443" {
		return nil, errors.New("custom URL ports are forbidden")
	}
	if strings.HasSuffix(strings.ToLower(u.Hostname()), ".local") || strings.EqualFold(u.Hostname(), "localhost") {
		return nil, errors.New("local host is forbidden")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !ip.IsGlobalUnicast() {
		return nil, errors.New("non-public IP forbidden")
	}
	return u, nil
}
func flowAllowedIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	// Exclude shared space, benchmarking, documentation ranges, and metadata.
	for _, cidr := range []string{"100.64.0.0/10", "169.254.0.0/16", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "2001:db8::/32", "fc00::/7", "fe80::/10", "::ffff:0:0/96"} {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return false
		}
	}
	return true
}
func flowFetchPublic(ctx context.Context, raw, format string) (string, error) {
	u, err := flowPublicURL(raw)
	if err != nil {
		return "", err
	}
	if format != "json" && format != "xml" {
		return "", errors.New("format must be json or xml")
	}
	resolver := net.DefaultResolver
	lookupCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ips, err := resolver.LookupIPAddr(lookupCtx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("could not resolve public URL host")
	}
	for _, candidate := range ips {
		if !flowAllowedIP(candidate.IP) {
			return "", errors.New("URL resolves to a forbidden network")
		}
	}
	ip := ips[0].IP
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxIdleConns: 0, TLSHandshakeTimeout: 5 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json, application/xml, text/xml")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP status %s", strconv.Itoa(resp.StatusCode))
	}
	if resp.ContentLength > flowHTTPMaxBytes {
		return "", errors.New("HTTP response too large")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, flowHTTPMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > flowHTTPMaxBytes {
		return "", errors.New("HTTP response too large")
	}
	if format == "json" {
		if !json.Valid(body) {
			return "", errors.New("response is not JSON")
		}
	} else {
		decoder := xml.NewDecoder(strings.NewReader(string(body)))
		started := false
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("invalid XML: %w", err)
			}
			if _, ok := token.(xml.StartElement); ok {
				started = true
			}
		}
		if !started {
			return "", errors.New("response is not XML")
		}
	}
	return string(body), nil
}
