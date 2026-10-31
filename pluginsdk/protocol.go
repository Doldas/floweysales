package pluginsdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ProtocolVersion is the wire protocol this SDK speaks. The backend refuses
// plugins whose manifest declares a protocol it does not support.
const ProtocolVersion = 1

// Methods a plugin can answer. Only "describe" is mandatory; the others are
// required exactly when the manifest lists the matching capability.
const (
	MethodDescribe   = "describe"
	MethodQuote      = "quote"
	MethodHistory    = "history"
	MethodSearch     = "search"
	MethodFinancials = "financials"
	MethodResearch   = "research"
	MethodForumPosts = "forum_posts"
)

// Capabilities a provider can declare.
const (
	CapQuote      = "quote"
	CapHistory    = "history"
	CapSearch     = "search"
	CapFinancials = "financials"
	CapResearch   = "research"
	CapForum      = "forum"
)

// Manifest describes a plugin. Each plugin ships it as plugin.json (embedded
// in the binary and copied into the store index), so the store, the
// installer and the running process agree on one description.
type Manifest struct {
	Protocol    int    `json:"protocol"`
	ID          string `json:"id"` // lowercase plugin id, e.g. "yahoo"
	Name        string `json:"name"`
	Version     string `json:"version"` // semantic version, e.g. "1.2.0"
	Description string `json:"description"`
	Publisher   string `json:"publisher"`
	Homepage    string `json:"homepage,omitempty"`
	License     string `json:"license,omitempty"`
	// Terms, when set, must be shown to and accepted by the administrator
	// before installation (for example unofficial upstream interfaces).
	Terms    string       `json:"terms,omitempty"`
	Provider ProviderInfo `json:"provider"`
	Env      []EnvVar     `json:"env,omitempty"`
}

// ProviderInfo is the market-data provider a plugin contributes.
type ProviderInfo struct {
	ID              string              `json:"id"` // uppercase provider id used by the API, e.g. "YAHOO"
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

// EnvVar is a configuration value the backend passes to the plugin process.
// Plugins never inherit the backend's environment; only declared names are
// forwarded (resolved from the environment or config.properties).
type EnvVar struct {
	Name        string `json:"name"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
}

// ResearchOperation is one read-only, allowlisted research call.
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
	versionPattern    = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?$`)
)

var knownCapabilities = map[string]bool{
	CapQuote: true, CapHistory: true, CapSearch: true, CapFinancials: true, CapResearch: true, CapForum: true,
}

// reservedEnv can never be forwarded to a plugin: they belong to the backend.
var reservedEnv = map[string]bool{
	"MYSQL_DSN": true, "AUTH_MYSQL_DSN": true, "FLOWEYSALES_SECRETS_KEY": true, "FLOWEYSALES_SECRETS_KEY_FILE": true,
	"AUTH_BOOTSTRAP_PASSWORD": true, "AUTH_BOOTSTRAP_EMAIL": true, "OPENAI_API_KEY": true,
	"BRAVE_SEARCH_API_KEY": true, "TAVILY_API_KEY": true, "SMTP_PASSWORD": true, "EMAIL_PASSWORD": true,
}

// Validate checks the manifest is well formed.
func (m Manifest) Validate() error {
	switch {
	case m.Protocol != ProtocolVersion:
		return fmt.Errorf("unsupported plugin protocol %d (want %d)", m.Protocol, ProtocolVersion)
	case !pluginIDPattern.MatchString(m.ID):
		return fmt.Errorf("invalid plugin id %q", m.ID)
	case strings.TrimSpace(m.Name) == "" || len(m.Name) > 80:
		return errors.New("plugin name must be 1-80 characters")
	case !versionPattern.MatchString(m.Version):
		return fmt.Errorf("plugin version %q is not a semantic version", m.Version)
	case !providerIDPattern.MatchString(m.Provider.ID):
		return fmt.Errorf("invalid provider id %q", m.Provider.ID)
	case len(m.Description) > 600 || len(m.Terms) > 4000 || len(m.Provider.Notes) > 1000:
		return errors.New("manifest text fields are too long")
	}
	if len(m.Provider.Capabilities) == 0 {
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
	seen := map[string]bool{}
	for _, op := range m.Provider.Research {
		if op.Operation == "" || seen[op.Operation] {
			return fmt.Errorf("invalid or duplicate research operation %q", op.Operation)
		}
		seen[op.Operation] = true
	}
	for _, e := range m.Env {
		if !envNamePattern.MatchString(e.Name) || reservedEnv[e.Name] || strings.HasPrefix(e.Name, "FLOWEYSALES_") {
			return fmt.Errorf("environment variable %q cannot be declared by a plugin", e.Name)
		}
	}
	return nil
}

// ParseManifest decodes and validates plugin.json content.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse plugin manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Request is one call from the backend.
type Request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers exactly one Request.
type Response struct {
	ID     uint64         `json:"id"`
	Result any            `json:"result,omitempty"`
	Error  *ResponseError `json:"error,omitempty"`
}

// Error codes. The backend maps them to HTTP statuses.
const (
	CodeUnsupported     = "unsupported"      // method or operation not offered
	CodeInvalidArgument = "invalid_argument" // bad symbol, range, dates…
	CodeNotConfigured   = "not_configured"   // missing API key or similar
	CodeUpstream        = "upstream"         // provider failed or returned garbage
	CodeInternal        = "internal"
)

// ResponseError is a failed call.
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ResponseError) Error() string { return e.Message }

// Errorf builds a coded error a handler can return.
func Errorf(code, format string, args ...any) error {
	return &ResponseError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Parameters of each method.
type (
	QuoteParams struct {
		Symbol string `json:"symbol"`
	}
	HistoryParams struct {
		Symbol string `json:"symbol"`
		Range  string `json:"range"`
	}
	SearchParams struct {
		Query string `json:"query"`
	}
	FinancialsParams struct {
		Symbol   string `json:"symbol"`
		FromYear int    `json:"from_year,omitempty"`
		ToYear   int    `json:"to_year,omitempty"`
	}
	ResearchParams struct {
		Operation string `json:"operation"`
		Symbol    string `json:"symbol,omitempty"`
		From      string `json:"from,omitempty"`
		To        string `json:"to,omitempty"`
	}
	ForumParams struct {
		Symbol string `json:"symbol"`
		Limit  int    `json:"limit"`
	}
)
