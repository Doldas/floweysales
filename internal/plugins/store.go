package plugins

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

// DefaultStoreURL is the official plugin index, generated from GitHub
// releases and published with the documentation site.
const DefaultStoreURL = "https://doldas.github.io/floweysales-plugins/index.json"

// Index is the store's catalogue.
type Index struct {
	Schema      int           `json:"schema"`
	GeneratedAt string        `json:"generated_at,omitempty"`
	Plugins     []IndexPlugin `json:"plugins"`
}

// IndexPlugin lists the published versions of one plugin.
type IndexPlugin struct {
	ID       string         `json:"id"`
	Versions []IndexVersion `json:"versions"`
}

// IndexVersion is one release: its manifest and a binary per platform
// ("linux-amd64", "windows-amd64", …).
type IndexVersion struct {
	Manifest   Manifest            `json:"manifest"`
	ReleasedAt string              `json:"released_at,omitempty"`
	Notes      string              `json:"notes,omitempty"`
	Artifacts  map[string]Artifact `json:"artifacts"`
}

// Artifact is a downloadable plugin binary pinned by its SHA-256.
type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

// Platform is this backend's artifact key.
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

const (
	maxIndexBytes    = 2 << 20
	maxArtifactBytes = 96 << 20
)

// storeLocation is a parsed store URL: an HTTPS URL, or a local file for
// offline and development stores.
type storeLocation struct {
	raw   string
	local bool
}

func parseStoreURL(raw string) (storeLocation, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return storeLocation{}, fmt.Errorf("invalid plugin store URL %q", raw)
	}
	switch u.Scheme {
	case "https":
		return storeLocation{raw: raw}, nil
	case "file":
		return storeLocation{raw: raw, local: true}, nil
	default:
		return storeLocation{}, fmt.Errorf("plugin store URL must use https:// or file:// (got %q)", u.Scheme)
	}
}

// fetch reads a store or artifact URL with a size cap. Local stores may
// reference local artifacts; HTTPS stores may only reference HTTPS ones, so a
// remote index can never make the backend read local files.
func (l storeLocation) fetch(ctx context.Context, client *http.Client, raw string, limit int64) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q", raw)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "file" && l.local:
		f, err := os.Open(fileURLPath(u))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return readCapped(f, limit)
	default:
		return nil, fmt.Errorf("refusing %s URL from this plugin store", u.Scheme)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FloweySales plugin manager")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", u.Host, resp.StatusCode)
	}
	return readCapped(resp.Body, limit)
}

// fileURLPath maps file:///C:/x to C:/x on Windows and keeps /x elsewhere.
func fileURLPath(u *url.URL) string {
	p := u.Path
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return p
}

func readCapped(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds %d MiB", limit>>20)
	}
	return data, nil
}

// parseIndex decodes and validates a store index; invalid versions are
// dropped (and reported) rather than failing the whole store.
func parseIndex(data []byte) (Index, []string, error) {
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, nil, fmt.Errorf("plugin store index is not valid JSON: %w", err)
	}
	if idx.Schema != 1 {
		return Index{}, nil, fmt.Errorf("unsupported plugin store schema %d", idx.Schema)
	}
	var warnings []string
	seen := map[string]bool{}
	out := idx.Plugins[:0]
	for _, p := range idx.Plugins {
		if !ValidPluginID(p.ID) || seen[p.ID] {
			warnings = append(warnings, fmt.Sprintf("skipped invalid or duplicate plugin %q", p.ID))
			continue
		}
		seen[p.ID] = true
		versions := p.Versions[:0]
		for _, v := range p.Versions {
			if err := validateVersion(p.ID, v); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s %s: %v", p.ID, v.Manifest.Version, err))
				continue
			}
			versions = append(versions, v)
		}
		p.Versions = versions
		if len(versions) > 0 {
			out = append(out, p)
		}
	}
	idx.Plugins = out
	return idx, warnings, nil
}

func validateVersion(id string, v IndexVersion) error {
	if v.Manifest.ID != id {
		return errors.New("manifest id does not match the store entry")
	}
	// Future protocols are kept so the UI can say an upgrade is needed.
	m := v.Manifest
	if m.Protocol > ProtocolVersion {
		m.Protocol = ProtocolVersion
	}
	if err := m.Validate(); err != nil {
		return err
	}
	for platform, a := range v.Artifacts {
		if sum, err := hex.DecodeString(a.SHA256); err != nil || len(sum) != 32 {
			return fmt.Errorf("artifact %s has no valid sha256", platform)
		}
		if a.URL == "" {
			return fmt.Errorf("artifact %s has no URL", platform)
		}
	}
	return nil
}

// find returns the plugin entry.
func (idx Index) find(id string) (IndexPlugin, bool) {
	for _, p := range idx.Plugins {
		if p.ID == id {
			return p, true
		}
	}
	return IndexPlugin{}, false
}

// installable reports whether this backend can run v.
func (v IndexVersion) installable() bool {
	_, ok := v.Artifacts[Platform()]
	return ok && v.Manifest.Protocol == ProtocolVersion
}

// Latest returns the newest installable stable version; a pre-release is
// only chosen when includePrerelease is set (the installed one is one).
func (p IndexPlugin) Latest(includePrerelease bool) (IndexVersion, bool) {
	var best IndexVersion
	found := false
	for _, v := range p.Versions {
		if !v.installable() || (IsPrerelease(v.Manifest.Version) && !includePrerelease) {
			continue
		}
		if !found || CompareVersions(v.Manifest.Version, best.Manifest.Version) > 0 {
			best, found = v, true
		}
	}
	return best, found
}

// Version returns a specific version.
func (p IndexPlugin) Version(version string) (IndexVersion, bool) {
	for _, v := range p.Versions {
		if v.Manifest.Version == version {
			return v, true
		}
	}
	return IndexVersion{}, false
}

// newest returns the newest version regardless of platform, for display.
func (p IndexPlugin) newest() IndexVersion {
	best := p.Versions[0]
	for _, v := range p.Versions[1:] {
		if CompareVersions(v.Manifest.Version, best.Manifest.Version) > 0 {
			best = v
		}
	}
	return best
}

// storeSnapshot is the last fetched index.
type storeSnapshot struct {
	index     Index
	warnings  []string
	checkedAt time.Time
	err       error
}
