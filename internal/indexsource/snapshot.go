// Package indexsource turns data a user supplies (a CSV file or their own
// Financial Modeling Prep key) into an index constituent list for that user.
// FloweySales ships no constituent list: nothing here is embedded, and a
// snapshot is stored per user, never in the repository. Missing data stays
// missing (a blank weight is nil, never 0) and no row is ever invented.
package indexsource

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	ProviderCSV = "csv"
	ProviderFMP = "fmp"
	DateLayout  = "2006-01-02"
	// MaxMembers bounds one list; the largest supported index has about 500.
	MaxMembers = 1000
)

// Spec is what an import is checked against. It comes from the registry entry
// of the index, never from the data being imported.
type Spec struct {
	ID      string
	Name    string
	Country string // ISO country; "US" lists carry no exchange suffix
	Nominal int    // the count in the index's name, 0 when unknown
}

// Member is one constituent.
type Member struct {
	Symbol   string   `json:"symbol"`
	Name     string   `json:"name,omitempty"`
	ISIN     string   `json:"isin,omitempty"`   // optional; the identity that keeps share classes and look-alike tickers apart
	Weight   *float64 `json:"weight,omitempty"` // percent as supplied; nil when not supplied
	Exchange string   `json:"exchange,omitempty"`
}

// Change is one dated membership change: Added joined and Removed left on Date.
type Change struct {
	Date    string `json:"date"`
	Added   string `json:"added,omitempty"`
	Removed string `json:"removed,omitempty"`
}

// Snapshot is one user's constituent list for one index.
type Snapshot struct {
	IndexID        string   `json:"index_id"`
	Provider       string   `json:"provider"`
	SourceName     string   `json:"source_name"`
	AsOf           string   `json:"as_of"`
	ImportedAt     string   `json:"imported_at"`
	LicenseTerms   string   `json:"license_terms"`
	LicenseConfirm bool     `json:"license_confirmed"`
	Members        []Member `json:"members"`
	Changes        []Change `json:"changes,omitempty"`
	PointInTime    bool     `json:"point_in_time"`
	HistoryFrom    string   `json:"history_from,omitempty"`
	Warnings       []string `json:"warnings"`
	// Unverified marks a list whose provider endpoint or response shape could
	// not be checked against the provider's documentation.
	Unverified     bool   `json:"unverified,omitempty"`
	UnverifiedNote string `json:"unverified_note,omitempty"`
}

// Symbols returns the current members in list order.
func (s Snapshot) Symbols() []string {
	out := make([]string, len(s.Members))
	for i, m := range s.Members {
		out[i] = m.Symbol
	}
	return out
}

// Key identifies the data for caches: a different import is a different key.
func (s Snapshot) Key() string {
	return s.Provider + ":" + s.AsOf + ":" + s.ImportedAt
}

// MembersAt returns the members on a date (YYYY-MM-DD, UTC) by undoing, from
// today's list, every change dated after it. known is false when the history
// does not reach that date (or there is none); the caller then falls back to
// today's members and must say so. A date on or after the list's as-of date
// is today's list, which is known.
func (s Snapshot) MembersAt(date string) (symbols []string, known bool) {
	if date >= s.AsOf {
		return s.Symbols(), true
	}
	if !s.PointInTime || s.HistoryFrom == "" || date < s.HistoryFrom {
		return s.Symbols(), false
	}
	set := map[string]bool{}
	for _, m := range s.Members {
		set[m.Symbol] = true
	}
	changes := append([]Change(nil), s.Changes...)
	sort.SliceStable(changes, func(a, b int) bool { return changes[a].Date > changes[b].Date })
	for _, c := range changes {
		if c.Date <= date || c.Date > s.AsOf {
			continue
		}
		if c.Added != "" {
			delete(set, c.Added)
		}
		if c.Removed != "" {
			set[c.Removed] = true
		}
	}
	for sym := range set {
		symbols = append(symbols, sym)
	}
	sort.Strings(symbols)
	return symbols, true
}

// EverMembers returns every symbol that was a member at any time from the
// given date through the as-of date: today's members plus those the changes
// show leaving after it. Without history it is today's members.
func (s Snapshot) EverMembers(from string) []string {
	seen := map[string]bool{}
	out := s.Symbols()
	for _, sym := range out {
		seen[sym] = true
	}
	if !s.PointInTime {
		return out
	}
	for _, c := range s.Changes {
		if c.Removed != "" && c.Date > from && !seen[c.Removed] {
			seen[c.Removed] = true
			out = append(out, c.Removed)
		}
	}
	return out
}

// Age returns whole days between the as-of date and now.
func (s Snapshot) Age(now time.Time) (int, bool) {
	t, err := time.Parse(DateLayout, s.AsOf)
	if err != nil {
		return 0, false
	}
	return int(now.Sub(t).Hours() / 24), true
}

// Confirm checks the licence acknowledgement every import needs.
func Confirm(terms string, confirmed bool) error {
	terms = strings.TrimSpace(terms)
	if !confirmed {
		return fmt.Errorf("you must confirm that you may use this data in FloweySales")
	}
	if len(terms) < 10 || len(terms) > 1000 {
		return fmt.Errorf("the licence or terms acknowledgement must be 10 to 1000 characters")
	}
	return nil
}
