package server

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"floweysales/internal/market"
	"floweysales/internal/storage"
)

func TestLeaderboardPresets(t *testing.T) {
	for _, u := range leaderboardUniverses {
		seen := map[string]bool{}
		if len(u.Symbols) == 0 || len(u.Symbols) > 30 {
			t.Errorf("%s: %d symbols", u.ID, len(u.Symbols))
		}
		for _, s := range u.Symbols {
			canonical, err := market.CanonicalSymbol(s)
			if err != nil || canonical != s || seen[s] {
				t.Errorf("%s: bad or duplicate symbol %q (%v)", u.ID, s, err)
			}
			seen[s] = true
		}
	}
	for _, s := range leaderboardStrategies {
		total := 0.0
		for f, w := range s.Weights {
			if _, ok := leaderboardFactorMetrics[f]; !ok {
				t.Errorf("%s: unknown factor %s", s.ID, f)
			}
			total += w
		}
		if math.Abs(total-1) > 1e-9 {
			t.Errorf("%s: weights sum to %v", s.ID, total)
		}
	}
	if s, ok := findLeaderboardStrategy(""); !ok || s.ID != "best" {
		t.Fatal("the empty strategy is best")
	}
}

// The Flow Studio dropdowns (investor-nodes.ts) write their options to this
// fixture; each must be one the server accepts, in the server's order.
func TestLeaderboardOptionsMatchFrontend(t *testing.T) {
	raw, err := os.ReadFile("testdata/leaderboard-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var options struct{ Universes, Strategies []string }
	if err := json.Unmarshal(raw, &options); err != nil {
		t.Fatal(err)
	}
	universes, strategies := []string{}, []string{}
	for _, u := range leaderboardUniverses {
		universes = append(universes, u.ID)
	}
	for _, s := range leaderboardStrategies {
		strategies = append(strategies, s.ID)
	}
	if strings.Join(options.Universes, ",") != strings.Join(universes, ",") || strings.Join(options.Strategies, ",") != strings.Join(strategies, ",") {
		t.Fatalf("frontend options %v differ from the server's %v %v", options, universes, strategies)
	}
}

func TestPercentilesRankWithTiesAndDirection(t *testing.T) {
	rows := []map[string]any{{"v": 10.0}, {"v": 30.0}, {"v": 20.0}, {"v": 20.0}, {}}
	up := percentiles(rows, "v", true)
	if up[0] != 0 || up[1] != 100 || up[2] != 50 || up[3] != 50 {
		t.Fatalf("higher is better: %v", up)
	}
	if _, ok := up[4]; ok {
		t.Fatal("a row without the field has no percentile")
	}
	if down := percentiles(rows, "v", false); down[0] != 100 || down[1] != 0 {
		t.Fatalf("lower is better: %v", down)
	}
	if one := percentiles(rows[:1], "v", true); one[0] != 50 {
		t.Fatalf("a single row is the median: %v", one)
	}
	if lone := percentiles([]map[string]any{{"v": 1.0}, {}}, "v", true); len(lone) != 0 {
		t.Fatalf("a metric only one row has compares with nothing: %v", lone)
	}
}

func TestScoreLeaderboardRanksExplainsAndFlags(t *testing.T) {
	good := map[string]any{"symbol": "GOOD", "price": 100.0, "pe_ttm": 15.0, "earnings_yield_pct": 6.67, "peg": 0.8, "eps_ttm": 6.6,
		"operating_margin_pct": 30.0, "net_margin_pct": 22.0, "fcf_margin_pct": 20.0, "gross_margin_pct": 55.0, "profitable_quarters": 4,
		"revenue_growth_pct": 18.0, "eps_growth_pct": 20.0, "revenue_accel_pct": 2.0, "momentum_12_1_pct": 30.0, "change_3m_pct": 8.0, "trend_points": 3,
		"above_sma200": true, "from_high_pct": -10.0, "pullback_fit": -2.0, "rsi14": 47.0, "rsi_fit": -2.0, "volatility_pct": 20.0, "max_drawdown_pct": -12.0}
	bad := map[string]any{"symbol": "BAD", "price": 50.0, "eps_ttm": -1.0, "earnings_yield_pct": -10.0,
		"operating_margin_pct": -5.0, "net_margin_pct": -8.0, "fcf_margin_pct": -3.0, "gross_margin_pct": 30.0, "profitable_quarters": 0,
		"revenue_growth_pct": -4.0, "momentum_12_1_pct": -20.0, "change_3m_pct": -9.0, "trend_points": 0,
		"above_sma200": false, "from_high_pct": -45.0, "pullback_fit": -33.0, "rsi14": 78.0, "rsi_fit": -33.0, "volatility_pct": 60.0, "max_drawdown_pct": -50.0}
	thin := map[string]any{"symbol": "THIN", "price": 10.0, "momentum_12_1_pct": 5.0, "change_3m_pct": 1.0, "trend_points": 2, "above_sma200": true,
		"from_high_pct": -5.0, "pullback_fit": -7.0, "rsi14": 55.0, "rsi_fit": -10.0, "volatility_pct": 30.0, "max_drawdown_pct": -20.0}
	failed := map[string]any{"symbol": "GONE", "error": "unknown symbol"}

	best, _ := findLeaderboardStrategy("best")
	ranked := scoreLeaderboard([]map[string]any{bad, thin, good, failed}, best)
	if len(ranked) != 3 {
		t.Fatalf("error rows are not ranked: %v", ranked)
	}
	if ranked[0]["symbol"] != "GOOD" || ranked[0]["rank"] != 1 || ranked[2]["symbol"] != "BAD" {
		t.Fatalf("unexpected order: %v %v %v", ranked[0]["symbol"], ranked[1]["symbol"], ranked[2]["symbol"])
	}
	if ranked[0]["value"].(float64) != 100 || ranked[0]["coverage_pct"].(float64) != 100 || ranked[0]["grade"] != "A" {
		t.Fatalf("GOOD should lead every factor: %v", ranked[0])
	}
	if reasons := ranked[0]["reasons"].(string); !strings.Contains(reasons, "P/E 15.0") || !strings.Contains(reasons, "revenue +18% y/y") {
		t.Fatalf("reasons should cite figures: %q", reasons)
	}
	flags := ranked[2]["flags"].(string)
	for _, want := range []string{"overbought", "below_sma200", "unprofitable", "shrinking_revenue"} {
		if !strings.Contains(flags, want) {
			t.Errorf("BAD flags %q miss %s", flags, want)
		}
	}
	if ranked[2]["value"].(float64) > 15 || ranked[2]["momentum"].(float64) > 35 {
		t.Fatalf("absolute caps must hold: %v", ranked[2])
	}
	if ranked[1]["coverage_pct"].(float64) >= 100 || !strings.Contains(ranked[1]["flags"].(string), "no_fundamentals") {
		t.Fatalf("THIN has no fundamentals: %v", ranked[1])
	}
	if _, ok := ranked[1]["quality"]; ok {
		t.Fatal("a missing factor is not reported as a score")
	}
}

func TestLeaderboardFlowNodeAndValidation(t *testing.T) {
	e := investorEngine(t)
	graph := `{"version":2,"nodes":[
		{"id":"start","type":"trigger","label":"Start"},
		{"id":"board","type":"leaderboard","label":"Board","symbols":"UP, DOWN, BAD","strategy":"momentum","limit":1},
		{"id":"top","type":"screen","label":"Top","rule":"score > 0 and grade != \"\""}],
		"edges":[{"from":"start","to":"board"},{"from":"board","to":"top"}]}`
	steps := runInvestorFlow(t, e, graph)
	var rows []map[string]any
	if err := json.Unmarshal([]byte(steps[1]["output"]), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("top 1 %v: %s", err, steps[1]["output"])
	}
	if rows[0]["symbol"] != "UP" || rows[0]["rank"].(float64) != 1 {
		t.Fatalf("UP has the stronger trend: %v", rows[0])
	}

	// A universe node with symbol rows in its input passes on only those rows,
	// ranked within the whole universe.
	focus := `{"version":2,"nodes":[
		{"id":"start","type":"trigger","label":"Start"},
		{"id":"filings","type":"sec_filings","label":"Filings","symbols":"DOWN","forms":"10-Q"},
		{"id":"board","type":"leaderboard","label":"Board","symbols":"UP, DOWN","strategy":"momentum","skip_empty":true}],
		"edges":[{"from":"start","to":"filings"},{"from":"filings","to":"board"}]}`
	steps = runInvestorFlow(t, e, focus)
	rows = nil
	if err := json.Unmarshal([]byte(steps[1]["output"]), &rows); err != nil || len(rows) != 1 || rows[0]["symbol"] != "DOWN" || rows[0]["rank"].(float64) != 2 {
		t.Fatalf("focus on filers %v: %s", err, steps[1]["output"])
	}

	for name, node := range map[string]string{
		"strategy":     `{"id":"b","type":"leaderboard","label":"B","strategy":"yolo"}`,
		"universe":     `{"id":"b","type":"leaderboard","label":"B","universe":"mars"}`,
		"limit":        `{"id":"b","type":"leaderboard","label":"B","limit":26}`,
		"misplaced":    `{"id":"b","type":"screen","label":"B","strategy":"best"}`,
		"non-investor": `{"id":"b","type":"output","label":"B","universe":"us_mega"}`,
	} {
		f := storage.AgentFlow{Name: "Board test", Graph: json.RawMessage(`{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},` + node + `],"edges":[{"from":"start","to":"b"}]}`)}
		if _, _, err := validateFlow(f); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	ok := storage.AgentFlow{Name: "Board test", Graph: json.RawMessage(`{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"b","type":"leaderboard","label":"B","universe":"se_large","strategy":"dip","limit":10}],"edges":[{"from":"start","to":"b"}]}`)}
	g, _, err := validateFlow(ok)
	if err != nil || !flowAllowedForNonAdmin(g) {
		t.Fatalf("a preset leaderboard is valid for everyone: %v", err)
	}
}

func TestLeaderboardEndpoint(t *testing.T) {
	e := investorEngine(t)
	get := func(query string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/leaderboard?"+query, nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}
	code, body := get("symbols=UP,DOWN,BAD&strategy=momentum")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	rows, unranked := body["rows"].([]any), body["unranked"].([]any)
	if len(rows) != 2 || len(unranked) != 1 || body["universe"] != "custom" || body["as_of"] == "" {
		t.Fatalf("unexpected body: %v", body)
	}
	if rows[0].(map[string]any)["symbol"] != "UP" || unranked[0].(map[string]any)["symbol"] != "BAD" {
		t.Fatalf("unexpected ranking: %v / %v", rows, unranked)
	}
	if len(body["universes"].([]any)) != len(leaderboardUniverses) || len(body["strategies"].([]any)) != len(leaderboardStrategies) {
		t.Fatal("the response lists the available universes and strategies")
	}
	// The second call reuses the cached metrics, so a strategy switch is free.
	e.providers = market.NoProviders{}
	if code, body := get("symbols=UP,DOWN,BAD&strategy=quality"); code != http.StatusOK || body["strategy"] != "quality" {
		t.Fatalf("cached call: %d %v", code, body)
	}
	if code, _ := get("strategy=yolo"); code != http.StatusBadRequest {
		t.Fatalf("unknown strategy: %d", code)
	}
	if code, _ := get("universe=mars"); code != http.StatusBadRequest {
		t.Fatalf("unknown universe: %d", code)
	}
}
