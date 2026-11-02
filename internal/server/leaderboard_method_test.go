package server

import (
	"strings"
	"testing"
)

func TestMethodologyHashPinsTheRules(t *testing.T) {
	best, _ := findLeaderboardStrategy("best")
	value, _ := findLeaderboardStrategy("value")
	a, b := leaderboardMethodology(best), leaderboardMethodology(best)
	if a["hash"] != b["hash"] || a["version"] != leaderboardMethodologyVersion {
		t.Fatalf("the same rules must hash the same: %v %v", a["hash"], b["hash"])
	}
	if a["hash"] == leaderboardMethodology(value)["hash"] {
		t.Fatal("different weights must change the hash")
	}
	changed := best
	changed.Weights = map[string]float64{"value": 1}
	if a["hash"] == leaderboardMethodology(changed)["hash"] {
		t.Fatal("edited weights must change the hash")
	}
}

func TestInputsHashIgnoresOrderButNotData(t *testing.T) {
	x := map[string]any{"symbol": "AAA", "price": 10.0}
	y := map[string]any{"symbol": "BBB", "price": 20.0}
	if leaderboardInputsHash([]map[string]any{x, y}) != leaderboardInputsHash([]map[string]any{y, x}) {
		t.Fatal("row order must not matter")
	}
	moved := map[string]any{"symbol": "BBB", "price": 20.5}
	if leaderboardInputsHash([]map[string]any{x, y}) == leaderboardInputsHash([]map[string]any{x, moved}) {
		t.Fatal("a changed input must change the hash")
	}
}

func TestRowsCarryConfidenceFreshnessAndAlternatives(t *testing.T) {
	// Strong trend, expensive, thin data, old quarter: every caveat should show.
	hot := map[string]any{"symbol": "HOT", "price": 100.0, "as_of": "2026-10-30", "ttm_through": "2026-03-31", "eps_ttm": 1.0, "earnings_yield_pct": 0.5,
		"momentum_12_1_pct": 80.0, "change_3m_pct": 20.0, "trend_points": 3, "above_sma200": true, "rsi14": 72.0, "rsi_fit": -27.0, "from_high_pct": -1.0, "pullback_fit": -11.0,
		"volatility_pct": 40.0, "max_drawdown_pct": -30.0}
	cheap := map[string]any{"symbol": "CHP", "price": 10.0, "as_of": "2026-10-30", "earnings_yield_pct": 12.0, "eps_ttm": 1.0, "momentum_12_1_pct": -10.0, "change_3m_pct": -5.0,
		"trend_points": 1, "above_sma200": false, "rsi14": 45.0, "rsi_fit": 0.0, "from_high_pct": -12.0, "pullback_fit": 0.0, "volatility_pct": 20.0, "max_drawdown_pct": -15.0}
	momentum, _ := findLeaderboardStrategy("momentum")
	ranked := scoreLeaderboard([]map[string]any{hot, cheap}, momentum)
	var row map[string]any
	for _, r := range ranked {
		if r["symbol"] == "HOT" {
			row = r
		}
	}
	if row["confidence"] != "low" {
		t.Errorf("thin data, a tiny universe and an old quarter should be low confidence: %v (%v)", row["confidence"], row["confidence_note"])
	}
	if age, _ := row["fundamentals_age_days"].(int); age < 200 {
		t.Errorf("fundamentals age = %v", row["fundamentals_age_days"])
	}
	if !strings.Contains(row["flags"].(string), "stale_fundamentals") {
		t.Errorf("flags = %v", row["flags"])
	}
	if alt := row["alternatives"].(string); alt == "" || !strings.Contains(alt, "old") {
		t.Errorf("alternatives = %q", alt)
	}
}

func TestProvenanceNamesDisclaimerAndDates(t *testing.T) {
	best, _ := findLeaderboardStrategy("best")
	rows := []map[string]any{{"symbol": "AAA", "as_of": "2026-10-29"}, {"symbol": "BBB", "as_of": "2026-10-30"}, {"symbol": "GONE", "error": "x"}}
	p := leaderboardProvenance(best, "custom", []string{"AAA", "BBB", "GONE"}, rows)
	dates := p["price_dates"].(map[string]string)
	if dates["newest"] != "2026-10-30" || dates["oldest"] != "2026-10-29" {
		t.Fatalf("dates = %v", dates)
	}
	if !strings.Contains(p["disclaimer"].(string), "not personalized financial advice") || p["inputs_hash"] == "" || p["universe_hash"] == "" {
		t.Fatalf("provenance = %v", p)
	}
}
