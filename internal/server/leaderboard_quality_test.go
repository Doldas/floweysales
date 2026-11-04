package server

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"floweysales/internal/provenance"
)

func TestProviderRecordURLOnlyWhenDeterministic(t *testing.T) {
	if u := providerRecordURL("YAHOO", "BRK-B"); u == nil || *u != "https://finance.yahoo.com/quote/BRK-B" {
		t.Fatalf("Yahoo has a page per symbol: %v", u)
	}
	if u := providerRecordURL("YAHOO", "^OMX"); u == nil || strings.Contains(*u, "^") {
		t.Fatalf("a symbol is escaped in the address: %v", u)
	}
	if providerRecordURL("SOMEPLUGIN", "AAPL") != nil || providerRecordURL("", "AAPL") != nil || providerRecordURL("YAHOO", "") != nil {
		t.Fatal("no link is made up for a provider whose page address is not known")
	}
}

func TestMetricsFactStatus(t *testing.T) {
	linked := metricsFact("YAHOO", "AAPL", "2026-11-03")
	if linked.Status != provenance.Known || linked.URL == "" || linked.AsOf == nil || linked.Validate() != nil {
		t.Errorf("linked: %+v", linked)
	}
	partial := metricsFact("OTHER", "AAPL", "2026-11-03")
	if partial.Status != provenance.Partial || partial.URL != "" || partial.Validate() != nil {
		t.Errorf("a provider without a known page is partial, not known: %+v", partial)
	}
	if none := metricsFact("", "AAPL", ""); none.Status != provenance.Unknown {
		t.Errorf("no provider is unknown: %+v", none)
	}
	if bad := metricsFact("YAHOO", "AAPL", "not a date"); bad.AsOf != nil {
		t.Error("an unreadable date is left out, not invented")
	}
}

func TestUniverseFactNeverClaimsMoreThanRecorded(t *testing.T) {
	asOf := "2026-09-30"
	curated := leaderboardUniverse{Source: universeSource{Kind: "curated", Name: "Curated", Rule: "r"}}
	if f := universeFact(curated); f.Kind != provenance.Assumed || f.Status != provenance.Partial || f.AsOf != nil {
		t.Errorf("a curated list with no date is partial: %+v", f)
	}
	curated.Source.AsOf = &asOf
	if f := universeFact(curated); f.Status != provenance.Known || f.AsOf == nil {
		t.Errorf("a dated list: %+v", f)
	}
	link := "https://www.sec.gov/x"
	disclosure := leaderboardUniverse{Source: universeSource{Kind: "disclosure", Name: "Filer (13F)", URL: &link, AsOf: &asOf}}
	if f := universeFact(disclosure); f.Kind != provenance.Observed || f.URL != link || f.Validate() != nil {
		t.Errorf("a disclosure is observed with its record: %+v", f)
	}
	if f := universeFact(leaderboardUniverse{Source: universeSource{Kind: "curated"}}); f.Status != provenance.Unknown {
		t.Errorf("no source name is unknown: %+v", f)
	}
}

func TestRankingFactIsCalculatedWithMethod(t *testing.T) {
	best, _ := findLeaderboardStrategy("best")
	f := rankingFact(best, rankingPreferences{}, "YAHOO", "us_mega", "2026-11-03", time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), "abc")
	if f.Kind != provenance.Calculated || f.Status != provenance.Known || f.Method == nil || f.Method.Version != leaderboardMethodologyVersion || f.Validate() != nil {
		t.Errorf("%+v", f)
	}
	if f := rankingFact(best, rankingPreferences{}, "YAHOO", "us_mega", "", time.Now().UTC(), "abc"); f.Status != provenance.Partial || f.AsOf != nil {
		t.Errorf("without a price date the fact is partial: %+v", f)
	}
}

func TestDataAgeStatesMixedAges(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	u := leaderboardUniverse{Source: universeSource{Kind: "curated", Name: "n"}}
	uniform := []map[string]any{
		{"symbol": "A", "as_of": "2026-11-03", "ttm_through": "2026-09-30"},
		{"symbol": "B", "as_of": "2026-11-03", "ttm_through": "2026-09-30"},
	}
	age := leaderboardDataAge(u, uniform, now)
	if age["mixed"] != false || !strings.Contains(strings.Join(age["statements"].([]string), " "), "Every price is from 2026-11-03") {
		t.Errorf("uniform data is not called mixed: %v", age)
	}

	mixed := []map[string]any{
		{"symbol": "A", "as_of": "2026-11-03", "ttm_through": "2026-09-30"},
		{"symbol": "B", "as_of": "2026-10-28", "ttm_through": "2026-03-31"},
		{"symbol": "C", "as_of": "2026-11-03"},
		{"symbol": "GONE", "error": "no data"},
	}
	age = leaderboardDataAge(u, mixed, now)
	text := strings.Join(age["statements"].([]string), " ")
	if age["mixed"] != true || age["older_than_newest"] != 1 || age["price_span_days"] != 6 {
		t.Errorf("one price is older, six days apart: %v", age)
	}
	for _, want := range []string{"mixes data ages", "1 of 3 companies", "2026-10-28 to 2026-11-03", "different dates, from 2026-03-31 to 2026-09-30", "1 of 3 companies have no reported quarter", "more than 200 days"} {
		if !strings.Contains(text, want) {
			t.Errorf("statements miss %q: %s", want, text)
		}
	}
	if age["stale_fundamentals"] != 1 || age["no_fundamentals"] != 1 {
		t.Errorf("counts: %v", age)
	}
}

func TestDataAgeFlagsAnOldDatasetAndListReview(t *testing.T) {
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	old := "2026-01-15"
	u := leaderboardUniverse{Source: universeSource{Kind: "curated", Name: "n", AsOf: &old}}
	rows := []map[string]any{{"symbol": "A", "as_of": "2026-11-03", "ttm_through": "2026-09-30"}}
	age := leaderboardDataAge(u, rows, now)
	text := strings.Join(age["statements"].([]string), " ")
	if !strings.Contains(text, "newest price is 17 days old") || !strings.Contains(text, "last reviewed") || age["list_stale"] != true {
		t.Errorf("an old dataset and an old list warn: %s", text)
	}

	undated := leaderboardUniverse{Source: universeSource{Kind: "curated", Name: "n"}}
	if text := strings.Join(leaderboardDataAge(undated, rows, now)["statements"].([]string), " "); !strings.Contains(text, "no recorded review date") {
		t.Errorf("an undated list says its age is unknown: %s", text)
	}
	period := "2026Q2"
	odd := leaderboardUniverse{Source: universeSource{Kind: "disclosure", Name: "n", AsOf: &period}}
	if text := strings.Join(leaderboardDataAge(odd, rows, now)["statements"].([]string), " "); !strings.Contains(text, "not checked") {
		t.Errorf("a period is not treated as a day: %s", text)
	}
	if text := strings.Join(leaderboardDataAge(undated, nil, now)["statements"].([]string), " "); !strings.Contains(text, "age of the data is unknown") {
		t.Errorf("no data is stated, not dated: %s", text)
	}
}

func TestBiasMeasuresOnlyWhatTheDataShows(t *testing.T) {
	u := leaderboardUniverse{Source: universeSource{Kind: "curated", Name: "n"}}
	symbols := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	ranked := []map[string]any{}
	for i, ch := range []float64{50, 40, 30, 10, -10, -20} {
		ranked = append(ranked, map[string]any{"symbol": symbols[i], "change_1y_pct": ch})
	}
	b := leaderboardBias(u, symbols, nil, ranked)
	m := b["measures"].(map[string]any)
	if b["point_in_time"] != false || m["unranked"] != 2 || m["top_n"] != 3 || m["with_1y_change"] != 6 {
		t.Fatalf("measures: %v", m)
	}
	if m["median_change_1y_pct_top"].(float64) != 40 || m["median_change_1y_pct_list"].(float64) != 20 || m["up_1y_pct"].(float64) != 66.7 {
		t.Errorf("medians: %v", m)
	}
	text := strings.Join(b["statements"].([]string), " ")
	for _, want := range []string{"survivors", "2 of 8 listed companies (25%)", "top 3", "not what it will earn", "one market"} {
		if !strings.Contains(text, want) {
			t.Errorf("statements miss %q: %s", want, text)
		}
	}

	// Too few companies: no comparison is invented.
	few := leaderboardBias(u, symbols[:3], nil, ranked[:3])
	if _, ok := few["measures"].(map[string]any)["median_change_1y_pct_top"]; ok {
		t.Error("a handful of companies gives no top-against-list comparison")
	}
	if !strings.Contains(strings.Join(few["statements"].([]string), " "), "Fewer than 6") {
		t.Error("the missing comparison is stated")
	}
}

func TestObjectiveTiltKeepsWeightsSumToOne(t *testing.T) {
	best, _ := findLeaderboardStrategy("best")
	if got := tiltedStrategy(best, rankingPreferences{}); got.Weights["risk"] != best.Weights["risk"] {
		t.Error("no objective leaves the strategy as it is")
	}
	if got := tiltedStrategy(best, rankingPreferences{Objective: "balanced"}); got.Weights["risk"] != best.Weights["risk"] {
		t.Error("balanced leaves the strategy as it is")
	}
	for _, obj := range []string{"preserve", "growth"} {
		got := tiltedStrategy(best, rankingPreferences{Objective: obj})
		total := 0.0
		for _, w := range got.Weights {
			total += w
		}
		if math.Abs(total-1) > 0.001 {
			t.Errorf("%s: weights sum to %v", obj, total)
		}
	}
	if p, g := tiltedStrategy(best, rankingPreferences{Objective: "preserve"}), tiltedStrategy(best, rankingPreferences{Objective: "growth"}); p.Weights["risk"] <= best.Weights["risk"] || g.Weights["growth"] <= best.Weights["growth"] {
		t.Error("protecting capital weighs risk more; growing capital weighs growth more")
	}
}

func TestPreferencesEnterTheMethodFingerprint(t *testing.T) {
	best, _ := findLeaderboardStrategy("best")
	plain := leaderboardMethodology(best)["hash"]
	if leaderboardMethodologyFor(best, rankingPreferences{})["hash"] != plain || leaderboardMethodologyFor(best, rankingPreferences{Objective: "balanced"})["hash"] != plain {
		t.Error("inactive preferences leave the fingerprint alone")
	}
	prefs := rankingPreferences{Objective: "preserve", MaxVolatilityPct: 30}
	a := leaderboardMethodologyFor(tiltedStrategy(best, prefs), prefs)
	b := leaderboardMethodologyFor(tiltedStrategy(best, prefs), prefs)
	if a["hash"] == plain || a["hash"] != b["hash"] {
		t.Errorf("preferences change the fingerprint, and the same ones give the same one: %v %v", a["hash"], plain)
	}
	other := rankingPreferences{Objective: "preserve", MaxVolatilityPct: 25}
	if leaderboardMethodologyFor(tiltedStrategy(best, other), other)["hash"] == a["hash"] {
		t.Error("a different constraint is a different fingerprint, even with the same weights")
	}
}

func TestConstrainNamesEveryCompanyRemoved(t *testing.T) {
	ranked := []map[string]any{
		{"symbol": "CALM", "volatility_pct": 18.0, "flags": "", "rank": 1},
		{"symbol": "WILD", "volatility_pct": 70.0, "flags": "", "rank": 2},
		{"symbol": "LOSS", "volatility_pct": 20.0, "flags": "unprofitable", "rank": 3},
		{"symbol": "NOFIN", "volatility_pct": 20.0, "flags": "no_fundamentals", "rank": 4},
		{"symbol": "NOVOL", "flags": "", "rank": 5},
		{"symbol": "NEVER", "volatility_pct": 10.0, "flags": "", "rank": 6},
		{"symbol": "LAST", "volatility_pct": 25.0, "flags": "", "rank": 7},
	}
	prefs := rankingPreferences{MaxVolatilityPct: 30, ExcludeUnprofitable: true, RequireFundamentals: true, ExcludeSymbols: []string{"NEVER"}}
	kept, removed := constrain(prefs, ranked)
	if len(kept) != 2 || kept[0]["symbol"] != "CALM" || kept[1]["symbol"] != "LAST" || kept[1]["rank"] != 2 {
		t.Fatalf("kept and renumbered: %v", kept)
	}
	if len(removed) != 5 || len(kept)+len(removed) != len(ranked) {
		t.Fatalf("every company is accounted for: %v", removed)
	}
	reasons := map[string]string{}
	for _, r := range removed {
		reasons[r.Symbol] = r.Reason
	}
	for symbol, want := range map[string]string{"WILD": "above your 30% limit", "LOSS": "not positive", "NOFIN": "no reported fundamentals", "NOVOL": "volatility unknown", "NEVER": "never-list"} {
		if !strings.Contains(reasons[symbol], want) || !strings.Contains(reasons[symbol], "Removed by your constraints") {
			t.Errorf("%s: %q should mention %q", symbol, reasons[symbol], want)
		}
	}
	if kept, removed := constrain(rankingPreferences{}, ranked); len(kept) != len(ranked) || len(removed) != 0 {
		t.Error("no constraints remove nothing")
	}

	sel := leaderboardSelection{Initial: 9, Steps: []selectionStep{{Step: "On the list", Count: 9}, {Step: "No usable data", Count: -2}, {Step: "Ranked", Count: 2}}}
	addConstraintStep(&sel, removed[:0])
	if len(sel.Steps) != 3 {
		t.Error("no removals add no step")
	}
	_, removed = constrain(prefs, ranked)
	addConstraintStep(&sel, removed)
	if len(sel.Steps) != 4 || sel.Steps[2].Count != -5 || sel.Steps[3].Step != "Ranked" || len(sel.Excluded) != 5 {
		t.Errorf("the pipeline names the removal before the final count: %+v", sel)
	}
}

func TestPreferencesValidation(t *testing.T) {
	p, err := rankingPreferences{Objective: " Growth ", ExcludeSymbols: []string{"msft", "AAPL", "aapl"}}.normalized()
	if err != nil || p.Objective != "growth" || strings.Join(p.ExcludeSymbols, ",") != "AAPL,MSFT" {
		t.Errorf("canonical form: %+v %v", p, err)
	}
	for name, bad := range map[string]rankingPreferences{
		"objective":  {Objective: "yolo"},
		"negative":   {MaxVolatilityPct: -1},
		"too large":  {MaxVolatilityPct: 500},
		"nan":        {MaxVolatilityPct: math.NaN()},
		"symbol":     {ExcludeSymbols: []string{"not a symbol!"}},
		"too many":   {ExcludeSymbols: make([]string, rankingMaxExcluded+1)},
		"empty item": {ExcludeSymbols: []string{""}},
	} {
		if _, err := bad.normalized(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if (rankingPreferences{Objective: "balanced"}).active() || (rankingPreferences{}).active() || !(rankingPreferences{ExcludeUnprofitable: true}).active() {
		t.Error("active reports whether anything changes")
	}
}

// memoryRankingPrefs stands in for the database.
type memoryRankingPrefs struct{ saved map[int64]string }

func (m *memoryRankingPrefs) LoadRankingPreferences(_ context.Context, id int64) (string, error) {
	return m.saved[id], nil
}
func (m *memoryRankingPrefs) SaveRankingPreferences(_ context.Context, id int64, p string) error {
	m.saved[id] = p
	return nil
}

func TestRankingPreferencesEndpointsAndLeaderboard(t *testing.T) {
	e := investorEngine(t)
	e.rankingPrefs = &memoryRankingPrefs{saved: map[int64]string{}}
	call := func(method, path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
		}
		e.mux.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	const board = "/api/market/leaderboard?symbols=UP,DOWN&strategy=momentum"

	code, plain := call(http.MethodGet, board, "")
	if code != http.StatusOK || plain["preferences"].(map[string]any)["applied"] != false || len(plain["rows"].([]any)) != 2 {
		t.Fatalf("no preferences saved: %d %v", code, plain)
	}
	if code, _ := call(http.MethodPut, "/api/market/leaderboard/preferences", `{"objective":"yolo"}`); code != http.StatusBadRequest {
		t.Fatalf("an unknown objective is refused: %d", code)
	}
	if code, _ := call(http.MethodPut, "/api/market/leaderboard/preferences", `{"objective":"growth","surprise":1}`); code != http.StatusBadRequest {
		t.Fatalf("unknown fields are refused: %d", code)
	}
	if code, saved := call(http.MethodPut, "/api/market/leaderboard/preferences", `{"objective":"preserve","exclude_symbols":["down"]}`); code != http.StatusOK || saved["summary"] == nil {
		t.Fatalf("save: %d %v", code, saved)
	}
	if code, got := call(http.MethodGet, "/api/market/leaderboard/preferences", ""); code != http.StatusOK || got["preferences"].(map[string]any)["objective"] != "preserve" {
		t.Fatalf("read back: %d %v", code, got)
	}

	_, with := call(http.MethodGet, board, "")
	prefs := with["preferences"].(map[string]any)
	rows := with["rows"].([]any)
	if prefs["applied"] != true || len(rows) != 1 || rows[0].(map[string]any)["symbol"] != "UP" {
		t.Fatalf("the never-list removes DOWN: %v", with)
	}
	sel := with["selection"].(map[string]any)
	if ex := sel["excluded"].([]any); len(ex) != 1 || !strings.Contains(ex[0].(map[string]any)["reason"].(string), "Removed by your constraints") {
		t.Errorf("the removal is named: %v", sel["excluded"])
	}
	weights := prefs["effective_weights"].(map[string]any)
	if weights["risk"].(float64) <= 0.1 {
		t.Errorf("preserving capital weighs risk more than the momentum strategy's 10%%: %v", weights)
	}
	hash := func(body map[string]any) any {
		return body["provenance"].(map[string]any)["methodology"].(map[string]any)["hash"]
	}
	_, off := call(http.MethodGet, board+"&preferences=off", "")
	if off["preferences"].(map[string]any)["applied"] != false || len(off["rows"].([]any)) != 2 || hash(off) != hash(plain) || hash(with) == hash(plain) {
		t.Errorf("turning preferences off restores the plain ranking and its fingerprint: %v", off["preferences"])
	}
	// Repeating the request repeats the fingerprint.
	_, again := call(http.MethodGet, board, "")
	if hash(again) != hash(with) {
		t.Error("the same preferences give the same fingerprint")
	}

	prov := with["provenance"].(map[string]any)
	for _, key := range []string{"fact", "universe_fact", "data_age", "bias"} {
		if prov[key] == nil {
			t.Errorf("provenance misses %q", key)
		}
	}
	ms := rows[0].(map[string]any)["metrics_source"].(map[string]any)
	if ms["fact"] == nil {
		t.Errorf("each row's numbers carry a fact: %v", ms)
	}
}

func TestRankingPreferencesWithoutStorage(t *testing.T) {
	e := investorEngine(t)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/market/leaderboard/preferences", strings.NewReader(`{"objective":"growth"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("saving without storage says so: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/leaderboard?symbols=UP,DOWN", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("the leaderboard works without preference storage: %d", rec.Code)
	}
}
