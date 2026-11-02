package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Reproducibility, freshness and humility for the leaderboard. A ranking is
// a function of three things: the methodology (factors, metrics, weights,
// caps), the universe and the input data. Each gets a version or a hash, so
// a score can be traced back, compared and re-run, and every row says how
// much to trust it and what could make the rank wrong.

// leaderboardMethodologyVersion changes whenever the scoring rules in
// leaderboard.go change (factor metrics, caps, missing-factor value, grades).
const leaderboardMethodologyVersion = "2026-11-1"

// leaderboardDisclaimer is shown wherever a ranking is.
const leaderboardDisclaimer = "Research suggestion, not personalized financial advice. Scores are percentiles within this list from provider data, not predictions; nothing here knows your goals, taxes or risk tolerance."

// leaderboardCapRules describes the absolute caps applied after ranking.
var leaderboardCapRules = []string{
	"value is capped at 15 when trailing EPS is not positive",
	"timing is capped at 20 when RSI(14) is 75 or higher",
	"momentum is capped at 35 when the price is below its 200-day average",
	"a factor with no data scores 40, below the median: unknown is not good",
}

const (
	// staleFundamentalsDays is the age of the newest reported quarter, at
	// the price date, beyond which fundamentals count as stale.
	staleFundamentalsDays = 200
	maxAlternatives       = 3
)

func shortHash(v any) string {
	raw, _ := json.Marshal(v) // maps marshal with sorted keys, so this is canonical
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

// leaderboardMethodology describes the scoring rules a strategy was ranked with.
func leaderboardMethodology(strategy leaderboardStrategy) map[string]any {
	metrics := map[string][]map[string]any{}
	for _, f := range leaderboardFactors {
		for _, m := range leaderboardFactorMetrics[f] {
			metrics[f] = append(metrics[f], map[string]any{"field": m.field, "higher_is_better": m.higher, "weight": m.weight})
		}
	}
	body := map[string]any{
		"version": leaderboardMethodologyVersion, "strategy": strategy.ID, "weights": strategy.Weights,
		"factors": leaderboardFactors, "metrics": metrics, "caps": leaderboardCapRules, "missing_factor_score": leaderboardMissingFactor,
	}
	body["hash"] = shortHash(body)
	return body
}

// leaderboardInputsHash fingerprints the data a ranking was computed from:
// the same hash with the same methodology and universe gives the same scores.
func leaderboardInputsHash(rows []map[string]any) string {
	sorted := append([]map[string]any(nil), rows...)
	sort.SliceStable(sorted, func(a, b int) bool { return fmt.Sprint(sorted[a]["symbol"]) < fmt.Sprint(sorted[b]["symbol"]) })
	return shortHash(sorted)
}

// leaderboardProvenance is what a ranking response adds so it can be
// reproduced and judged: methodology, universe, input fingerprint, freshness
// and the disclaimer.
func leaderboardProvenance(strategy leaderboardStrategy, universe string, symbols []string, rows []map[string]any) map[string]any {
	asOf, oldest := "", ""
	for _, row := range rows {
		if _, failed := row["error"]; failed {
			continue
		}
		if d, ok := row["as_of"].(string); ok {
			if d > asOf {
				asOf = d
			}
			if oldest == "" || d < oldest {
				oldest = d
			}
		}
	}
	return map[string]any{
		"methodology":   leaderboardMethodology(strategy),
		"universe_id":   universe,
		"universe_hash": shortHash(symbols),
		"inputs_hash":   leaderboardInputsHash(rows),
		"price_dates":   map[string]string{"newest": asOf, "oldest": oldest},
		"disclaimer":    leaderboardDisclaimer,
	}
}

// fundamentalsAgeDays is how old the newest reported quarter is at the price
// date. Both dates come from the data, never the clock, so it is repeatable.
func fundamentalsAgeDays(row map[string]any) (int, bool) {
	through, _ := row["ttm_through"].(string)
	asOf, _ := row["as_of"].(string)
	a, errA := time.Parse("2006-01-02", through)
	b, errB := time.Parse("2006-01-02", asOf)
	if errA != nil || errB != nil {
		return 0, false
	}
	return int(b.Sub(a).Hours() / 24), true
}

// leaderboardConfidence rates how far a score can be trusted, from how many
// factors have data, how many companies it is compared with and how fresh
// the inputs are.
func leaderboardConfidence(coveragePct float64, universeSize int, flags []string) (level, note string) {
	level = "high"
	var why []string
	has := func(flag string) bool {
		for _, f := range flags {
			if f == flag {
				return true
			}
		}
		return false
	}
	switch {
	case coveragePct < 67:
		level = "low"
		why = append(why, fmt.Sprintf("only %.0f%% of the factors have data", coveragePct))
	case coveragePct < 100:
		level = "medium"
		why = append(why, fmt.Sprintf("%.0f%% of the factors have data", coveragePct))
	}
	if universeSize < 8 {
		if level == "high" {
			level = "medium"
		}
		why = append(why, fmt.Sprintf("only %d companies to compare with", universeSize))
	}
	if has("stale_price") || has("stale_fundamentals") {
		why = append(why, "some inputs are old")
		level = map[string]string{"high": "medium", "medium": "low", "low": "low"}[level]
	}
	if len(why) == 0 {
		return level, "All six factors have data on fresh inputs."
	}
	return level, strings.Join(why, "; ")
}

// leaderboardAlternatives names the strongest cases against a rank, so a
// reader weighs a competing explanation before acting on the score.
func leaderboardAlternatives(row map[string]any, fs map[string]float64, strategy leaderboardStrategy, flags []string) []string {
	var out []string
	if m, ok := fs["momentum"]; ok && m >= 75 {
		if v, ok := fs["value"]; ok && v < 40 {
			out = append(out, fmt.Sprintf("The trend may already price in the good news: momentum %.0f but value only %.0f", m, v))
		}
	}
	for _, flag := range flags {
		switch flag {
		case "overbought":
			out = append(out, "Overbought: waiting for a pullback may give a better entry")
		case "unprofitable":
			out = append(out, "Negative earnings: a low price may be a value trap")
		case "shrinking_revenue":
			out = append(out, "Shrinking revenue: the business may be getting worse, not cheaper")
		case "stale_fundamentals":
			out = append(out, "The latest reported quarter is old, so the fundamentals may have changed")
		}
	}
	type weak struct {
		factor string
		score  float64
		weight float64
		known  bool
	}
	var weakest []weak
	for _, f := range leaderboardFactors {
		w := strategy.Weights[f]
		if w < 0.15 {
			continue
		}
		v, known := fs[f]
		if !known || v < 40 {
			weakest = append(weakest, weak{f, v, w, known})
		}
	}
	sort.Slice(weakest, func(a, b int) bool { return weakest[a].weight > weakest[b].weight })
	for _, w := range weakest {
		if w.known {
			out = append(out, fmt.Sprintf("%s scores only %.0f, yet this strategy weighs it %.0f%%", w.factor, w.score, w.weight*100))
		} else {
			out = append(out, fmt.Sprintf("%s has no data, yet this strategy weighs it %.0f%%; the score assumes a below-median value", w.factor, w.weight*100))
		}
	}
	if len(out) > maxAlternatives {
		out = out[:maxAlternatives]
	}
	return out
}

// annotateRank adds confidence, freshness and alternative explanations to a
// ranked entry and returns the flags including any freshness flags.
func annotateRank(entry, row map[string]any, fs map[string]float64, strategy leaderboardStrategy, flags []string, universeSize int) []string {
	if age, ok := fundamentalsAgeDays(row); ok {
		entry["fundamentals_age_days"] = age
		if age > staleFundamentalsDays {
			flags = append(flags, "stale_fundamentals")
		}
	}
	coverage, _ := entry["coverage_pct"].(float64)
	level, note := leaderboardConfidence(math.Round(coverage), universeSize, flags)
	entry["confidence"], entry["confidence_note"] = level, note
	entry["alternatives"] = strings.Join(leaderboardAlternatives(row, fs, strategy, flags), "; ")
	return flags
}
