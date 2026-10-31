// Package plugins runs market-data provider plugins and manages their
// installation from a plugin store.
//
// A plugin is a standalone executable. The backend starts it on first use,
// with a minimal environment plus the variables its manifest declares, and
// talks to it over newline-delimited JSON on stdin/stdout (see
// docs/plugins.md). The store is a JSON index of plugin versions with one
// SHA-256-pinned artifact per platform; installing downloads, verifies and
// handshakes with the binary before it replaces anything.
package plugins

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ProtocolVersion is the wire protocol this backend speaks.
const ProtocolVersion = 1

// Capabilities a provider can declare; they mirror pluginsdk.
const (
	CapQuote      = "quote"
	CapHistory    = "history"
	CapSearch     = "search"
	CapFinancials = "financials"
	CapResearch   = "research"
	CapForum      = "forum"
)

// Manifest is a plugin's plugin.json; see pluginsdk.Manifest.
type Manifest struct {
	Protocol    int          `json:"protocol"`
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description"`
	Publisher   string       `json:"publisher"`
	Homepage    string       `json:"homepage,omitempty"`
	License     string       `json:"license,omitempty"`
	Terms       string       `json:"terms,omitempty"`
	Provider    ProviderInfo `json:"provider"`
	Env         []EnvVar     `json:"env,omitempty"`
}

// ProviderInfo is the provider a plugin contributes.
type ProviderInfo struct {
	ID              string              `json:"id"`
	Label           string              `json:"label"`
	OfficialAPI     bool                `json:"official_api"`
	Experimental    bool                `json:"experimental"`
	Capabilities    []string            `json:"capabilities"`
	QuoteKind       string              `json:"quote_kind"`
	MinPollSeconds  int                 `json:"min_poll_seconds"`
	CacheTTLSeconds int                 `json:"cache_ttl_seconds"`
	Notes           string              `json:"notes"`
	Research        []ResearchOperation `json:"research,omitempty"`
}

// EnvVar is a configuration value forwarded to the plugin process.
type EnvVar struct {
	Name        string `json:"name"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
}

// ResearchOperation is one allowlisted read-only research call.
type ResearchOperation struct {
	Operation      string `json:"operation"`
	Path           string `json:"path"`
	Description    string `json:"description"`
	SymbolRequired bool   `json:"symbol_required"`
	DatesRequired  bool   `json:"dates_required,omitempty"`
}

// HasCapability reports whether the provider declares c.
func (p ProviderInfo) HasCapability(c string) bool {
	for _, have := range p.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

var (
	pluginIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)
	providerIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,31}$`)
	envNamePattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
)

var knownCapabilities = map[string]bool{
	CapQuote: true, CapHistory: true, CapSearch: true, CapFinancials: true, CapResearch: true, CapForum: true,
}

// reservedEnv are backend secrets that a plugin can never ask for.
var reservedEnv = map[string]bool{
	"MYSQL_DSN": true, "AUTH_MYSQL_DSN": true, "FLOWEYSALES_SECRETS_KEY": true, "FLOWEYSALES_SECRETS_KEY_FILE": true,
	"AUTH_BOOTSTRAP_PASSWORD": true, "AUTH_BOOTSTRAP_EMAIL": true, "OPENAI_API_KEY": true,
	"BRAVE_SEARCH_API_KEY": true, "TAVILY_API_KEY": true, "SMTP_PASSWORD": true, "EMAIL_PASSWORD": true,
}

// ValidPluginID reports whether id is a well-formed plugin id; it is also a
// directory name, so the pattern excludes separators and dots.
func ValidPluginID(id string) bool { return pluginIDPattern.MatchString(id) }

// Validate checks everything the backend relies on: identifiers that become
// paths and routing keys, the protocol, and that no backend secret is
// requested.
func (m Manifest) Validate() error {
	switch {
	case m.Protocol != ProtocolVersion:
		return fmt.Errorf("plugin protocol %d is not supported (this backend speaks %d)", m.Protocol, ProtocolVersion)
	case !ValidPluginID(m.ID):
		return fmt.Errorf("invalid plugin id %q", m.ID)
	case strings.TrimSpace(m.Name) == "" || len(m.Name) > 80:
		return errors.New("plugin name must be 1-80 characters")
	case !ValidVersion(m.Version):
		return fmt.Errorf("plugin version %q is not a semantic version", m.Version)
	case !providerIDPattern.MatchString(m.Provider.ID):
		return fmt.Errorf("invalid provider id %q", m.Provider.ID)
	case len(m.Description) > 600 || len(m.Terms) > 4000 || len(m.Provider.Notes) > 1000:
		return errors.New("manifest text fields are too long")
	case len(m.Provider.Capabilities) == 0:
		return errors.New("provider declares no capabilities")
	}
	for _, c := range m.Provider.Capabilities {
		if !knownCapabilities[c] {
			return fmt.Errorf("unknown capability %q", c)
		}
	}
	if m.Provider.HasCapability(CapResearch) != (len(m.Provider.Research) > 0) {
		return errors.New("research capability and research operations must be declared together")
	}
	for _, e := range m.Env {
		if !envNamePattern.MatchString(e.Name) || reservedEnv[e.Name] || strings.HasPrefix(e.Name, "FLOWEYSALES_") {
			return fmt.Errorf("environment variable %q cannot be declared by a plugin", e.Name)
		}
	}
	return nil
}
