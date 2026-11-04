package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"floweysales/internal/market"
	"floweysales/internal/storage"
)

// A user's own objective and constraints, applied to the leaderboard. They
// belong to the user, not to the system: an administrator only sets up the
// service. The objective tilts the strategy's factor weights by documented
// multipliers; constraints remove companies after scoring, so every score
// keeps meaning "better than most of this list" and every company removed is
// named with the rule that removed it. Both are part of the method
// fingerprint, so a ranking made with preferences can be told apart from, and
// repeated apart from, one made without. Flow nodes and the agent tool do not
// apply them: an automation's ranking must not change with who last edited a
// setting.

const (
	rankingMaxVolatilityLimit = 200.0
	rankingMaxExcluded        = 50
)

// objectiveTilts says how each objective scales a factor's weight before the
// weights are made to sum to 1 again. An objective not listed here ("" or
// "balanced") leaves the strategy as it is.
var objectiveTilts = map[string]map[string]float64{
	"preserve": {"value": 1.2, "quality": 1.3, "growth": 0.6, "momentum": 0.6, "timing": 1, "risk": 2},
	"growth":   {"value": 0.8, "quality": 1, "growth": 1.4, "momentum": 1.2, "timing": 1, "risk": 0.6},
}

var objectiveLabels = map[string]string{
	"":         "No objective: the strategy's own weights",
	"balanced": "Balanced: the strategy's own weights",
	"preserve": "Protect capital: more weight on low risk, quality and value; less on growth and momentum",
	"growth":   "Grow capital: more weight on growth and momentum; less on low risk and value",
}

// rankingPreferences is what one user asked the leaderboard to respect.
type rankingPreferences struct {
	Objective           string   `json:"objective"`
	MaxVolatilityPct    float64  `json:"max_volatility_pct,omitempty"` // 0 = no limit
	ExcludeUnprofitable bool     `json:"exclude_unprofitable,omitempty"`
	RequireFundamentals bool     `json:"require_fundamentals,omitempty"`
	ExcludeSymbols      []string `json:"exclude_symbols,omitempty"`
}

// active reports whether the preferences change anything.
func (p rankingPreferences) active() bool {
	return (p.Objective != "" && p.Objective != "balanced") || p.MaxVolatilityPct > 0 || p.ExcludeUnprofitable || p.RequireFundamentals || len(p.ExcludeSymbols) > 0
}

// normalized returns the preferences in canonical form or says what is wrong.
func (p rankingPreferences) normalized() (rankingPreferences, error) {
	p.Objective = strings.ToLower(strings.TrimSpace(p.Objective))
	if _, ok := objectiveLabels[p.Objective]; !ok {
		return p, fmt.Errorf("unknown objective %q", p.Objective)
	}
	if math.IsNaN(p.MaxVolatilityPct) || p.MaxVolatilityPct < 0 || p.MaxVolatilityPct > rankingMaxVolatilityLimit {
		return p, fmt.Errorf("the volatility limit must be between 0 and %.0f percent (0 means no limit)", rankingMaxVolatilityLimit)
	}
	if len(p.ExcludeSymbols) > rankingMaxExcluded {
		return p, fmt.Errorf("at most %d excluded symbols", rankingMaxExcluded)
	}
	seen := map[string]bool{}
	symbols := []string{}
	for _, raw := range p.ExcludeSymbols {
		s, err := market.CanonicalSymbol(raw)
		if err != nil {
			return p, fmt.Errorf("excluded symbol %q: %w", raw, err)
		}
		if !seen[s] {
			seen[s] = true
			symbols = append(symbols, s)
		}
	}
	sort.Strings(symbols)
	p.ExcludeSymbols = symbols
	return p, nil
}

// describe lists, in words, what the preferences do; it is shown with the
// ranking and enters the method fingerprint.
func (p rankingPreferences) describe() []string {
	var out []string
	if p.Objective != "" && p.Objective != "balanced" {
		out = append(out, "Objective: "+objectiveLabels[p.Objective])
	}
	if p.MaxVolatilityPct > 0 {
		out = append(out, fmt.Sprintf("Only companies with volatility of %.0f%% a year or less", p.MaxVolatilityPct))
	}
	if p.ExcludeUnprofitable {
		out = append(out, "Without companies whose trailing earnings are not positive")
	}
	if p.RequireFundamentals {
		out = append(out, "Only companies with reported fundamentals")
	}
	if len(p.ExcludeSymbols) > 0 {
		out = append(out, "Never: "+strings.Join(p.ExcludeSymbols, ", "))
	}
	return out
}

// tiltedStrategy applies the objective to a strategy's weights. The weights
// are renormalized and rounded to four decimals so they sum to one and the
// same preferences always give the same numbers.
func tiltedStrategy(s leaderboardStrategy, p rankingPreferences) leaderboardStrategy {
	tilt, ok := objectiveTilts[p.Objective]
	if !ok {
		return s
	}
	total := 0.0
	scaled := map[string]float64{}
	for f, w := range s.Weights {
		scaled[f] = w * tilt[f]
		total += scaled[f]
	}
	out := s
	out.Weights = map[string]float64{}
	for f, w := range scaled {
		out.Weights[f] = math.Round(w/total*10000) / 10000
	}
	return out
}

// constrain removes the ranked companies the preferences refuse and says why.
// Scores are untouched; ranks are renumbered over what remains. A volatility
// limit cannot be checked for a company with no volatility figure, so that
// company is removed and the reason says it was unknown.
func constrain(p rankingPreferences, ranked []map[string]any) (kept []map[string]any, removed []excludedCompany) {
	never := map[string]bool{}
	for _, s := range p.ExcludeSymbols {
		never[s] = true
	}
	kept = []map[string]any{}
	for _, row := range ranked {
		symbol := fmt.Sprint(row["symbol"])
		flags := fmt.Sprint(row["flags"])
		reason := ""
		switch {
		case never[symbol]:
			reason = "on your never-list"
		case p.ExcludeUnprofitable && strings.Contains(flags, "unprofitable"):
			reason = "trailing earnings are not positive"
		case p.RequireFundamentals && strings.Contains(flags, "no_fundamentals"):
			reason = "no reported fundamentals"
		case p.MaxVolatilityPct > 0:
			if v, ok := row["volatility_pct"].(float64); !ok {
				reason = fmt.Sprintf("volatility unknown, so your %.0f%% limit cannot be checked", p.MaxVolatilityPct)
			} else if v > p.MaxVolatilityPct {
				reason = fmt.Sprintf("volatility %.0f%% is above your %.0f%% limit", v, p.MaxVolatilityPct)
			}
		}
		if reason != "" {
			removed = append(removed, excludedCompany{Symbol: symbol, Reason: "Removed by your constraints: " + reason})
			continue
		}
		kept = append(kept, row)
	}
	for i, row := range kept {
		row["rank"] = i + 1
	}
	return kept, removed
}

// addConstraintStep records the companies the user's constraints removed in
// the pipeline from list to ranking, before the final count.
func addConstraintStep(sel *leaderboardSelection, removed []excludedCompany) {
	if len(removed) == 0 {
		return
	}
	sel.Excluded = append(sel.Excluded, removed...)
	sort.Slice(sel.Excluded, func(a, b int) bool { return sel.Excluded[a].Symbol < sel.Excluded[b].Symbol })
	step := selectionStep{Step: "Removed by your constraints", Count: -len(removed), Detail: "Companies your saved constraints do not allow. Their scores are unchanged."}
	last := len(sel.Steps) - 1
	sel.Steps = append(sel.Steps[:last:last], step, sel.Steps[last])
}

// rankingPreferencesStore keeps each user's leaderboard preferences.
type rankingPreferencesStore interface {
	LoadRankingPreferences(ctx context.Context, userID int64) (string, error)
	SaveRankingPreferences(ctx context.Context, userID int64, prefs string) error
}

func (e *Engine) rankingPrefsStore() rankingPreferencesStore {
	if e.rankingPrefs != nil {
		return e.rankingPrefs
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

// loadRankingPreferences reads the caller's saved preferences. A caller who
// is not signed in (authentication on) has none; saved data that no longer
// validates is ignored rather than guessed at.
func (e *Engine) loadRankingPreferences(r *http.Request) rankingPreferences {
	store := e.rankingPrefsStore()
	if store == nil {
		return rankingPreferences{}
	}
	var id int64
	if e.authService != nil {
		u, ok := e.currentUser(r)
		if !ok {
			return rankingPreferences{}
		}
		id = u.ID
	}
	raw, err := store.LoadRankingPreferences(r.Context(), id)
	if err != nil || raw == "" {
		return rankingPreferences{}
	}
	var p rankingPreferences
	if json.Unmarshal([]byte(raw), &p) != nil {
		return rankingPreferences{}
	}
	p, err = p.normalized()
	if err != nil {
		return rankingPreferences{}
	}
	return p
}

// registerRankingPreferenceRoutes serves the caller's own preferences.
func (e *Engine) registerRankingPreferenceRoutes() {
	e.mux.HandleFunc("GET /api/market/leaderboard/preferences", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := e.planUser(w, r); !ok {
			return
		}
		p := e.loadRankingPreferences(r)
		writeJSON(w, http.StatusOK, map[string]any{"preferences": p, "objectives": objectiveLabels, "summary": p.describe(), "stored": e.rankingPrefsStore() != nil})
	})
	e.mux.HandleFunc("PUT /api/market/leaderboard/preferences", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		var p rankingPreferences
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, storage.RankingPreferencesMaxBytes))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid preferences"})
			return
		}
		p, err := p.normalized()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		store := e.rankingPrefsStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Preference storage unavailable"})
			return
		}
		raw, _ := json.Marshal(p)
		if err := store.SaveRankingPreferences(r.Context(), u.ID, string(raw)); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Cannot save the preferences"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"preferences": p, "objectives": objectiveLabels, "summary": p.describe(), "stored": true})
	})
}
