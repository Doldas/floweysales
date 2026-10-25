// Package updates checks for newer FloweySales container images and hands
// approved updates to the host-side updater. The application never pulls
// images, runs migrations or restarts containers itself.
package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// RevisionLabel is the OCI label carrying the git commit an image was built from.
const RevisionLabel = "org.opencontainers.image.revision"

const maxRegistryBody = 1 << 20

var (
	imagePattern = regexp.MustCompile(`^[a-z0-9.-]+(:[0-9]+)?(/[a-z0-9._-]+)+$`)
	tagPattern   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

// ImageInfo describes the image a registry tag currently points to.
type ImageInfo struct {
	Image   string `json:"image"`
	Tag     string `json:"tag"`
	Digest  string `json:"digest"`
	Commit  string `json:"commit,omitempty"`
	Created string `json:"created,omitempty"`
}

// Registry reads public image metadata from an OCI distribution registry
// using anonymous pull tokens. BaseURL overrides https://<host> for tests.
type Registry struct {
	Client  *http.Client
	BaseURL string
}

// ParseImage splits "ghcr.io/owner/name" into registry host and repository.
func ParseImage(image string) (host, repository string, err error) {
	if !imagePattern.MatchString(image) {
		return "", "", fmt.Errorf("invalid image reference %q", image)
	}
	host, repository, _ = strings.Cut(image, "/")
	return host, repository, nil
}

// ValidTag reports whether tag is a valid OCI tag.
func ValidTag(tag string) bool { return tagPattern.MatchString(tag) }

func (r Registry) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (r Registry) base(host string) string {
	if r.BaseURL != "" {
		return strings.TrimRight(r.BaseURL, "/")
	}
	return "https://" + host
}

func (r Registry) getJSON(ctx context.Context, rawURL, token, accept string, out any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := r.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry returned HTTP %d for %s", res.StatusCode, req.URL.Path)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxRegistryBody))
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return "", fmt.Errorf("invalid registry response: %w", err)
	}
	return res.Header.Get("Docker-Content-Digest"), nil
}

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Platform  *struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform"`
}

type manifest struct {
	MediaType string       `json:"mediaType"`
	Manifests []descriptor `json:"manifests"`
	Config    descriptor   `json:"config"`
}

const manifestAccept = "application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json," +
	"application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json"

// Lookup resolves image:tag to the linux manifest for this architecture and
// reads the commit and creation time from its config.
func (r Registry) Lookup(ctx context.Context, image, tag string) (ImageInfo, error) {
	info := ImageInfo{Image: image, Tag: tag}
	host, repository, err := ParseImage(image)
	if err != nil {
		return info, err
	}
	if !ValidTag(tag) {
		return info, fmt.Errorf("invalid image tag %q", tag)
	}
	base := r.base(host)
	var token struct {
		Token string `json:"token"`
	}
	tokenURL := base + "/token?scope=" + url.QueryEscape("repository:"+repository+":pull")
	if _, err := r.getJSON(ctx, tokenURL, "", "", &token); err != nil {
		return info, fmt.Errorf("registry token: %w", err)
	}
	var top manifest
	digest, err := r.getJSON(ctx, base+"/v2/"+repository+"/manifests/"+url.PathEscape(tag), token.Token, manifestAccept, &top)
	if err != nil {
		return info, err
	}
	info.Digest = digest
	current := top
	if len(top.Manifests) > 0 {
		chosen := ""
		for _, m := range top.Manifests {
			if m.Platform != nil && m.Platform.OS == "linux" && m.Platform.Architecture == runtime.GOARCH {
				chosen = m.Digest
				break
			}
		}
		if chosen == "" {
			return info, fmt.Errorf("image %s:%s has no linux/%s variant", image, tag, runtime.GOARCH)
		}
		if _, err := r.getJSON(ctx, base+"/v2/"+repository+"/manifests/"+chosen, token.Token, manifestAccept, &current); err != nil {
			return info, err
		}
	}
	if current.Config.Digest == "" {
		return info, fmt.Errorf("image manifest has no config")
	}
	var config struct {
		Created string `json:"created"`
		Config  struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if _, err := r.getJSON(ctx, base+"/v2/"+repository+"/blobs/"+current.Config.Digest, token.Token, "", &config); err != nil {
		return info, err
	}
	info.Created = config.Created
	if commit := config.Config.Labels[RevisionLabel]; commitPattern.MatchString(commit) {
		info.Commit = commit
	}
	return info, nil
}
