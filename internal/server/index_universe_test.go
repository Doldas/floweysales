package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"floweysales/internal/indexsource/fixture"
)

// Synthetic lists for the three initial indices (#258). Nothing here is real
// index data: the companies, tickers and ISINs are invented by the fixture.

func importIndex(t *testing.T, e *Engine, id string, rows []fixture.Row, history string) {
	t.Helper()
	body := csvBody(fixture.CSV(rows))
	if history != "" {
		body["history_csv"] = history
	}
	if code, out, raw := icall(e, "PUT", "/api/market/indices/"+id+"/csv", body); code != http.StatusOK {
		t.Fatalf("import %s: %d %v %s", id, code, out["problems"], raw)
	}
}

func suppliedAll(t *testing.T) *Engine {
	t.Helper()
	e, _ := indexTestEngine(t)
	importIndex(t, e, "index_sp500", fixture.Rows("SPA", "", 500, 1000, "US"), "")
	importIndex(t, e, "index_nasdaq100", fixture.Rows("NDA", "", 100, 5000, "US"), "")
	importIndex(t, e, "index_omxs30", fixture.Rows("OMA", ".ST", 30, 9000, "SE"), "")
	return e
}

func TestAllThreeIndicesActivateAndReportVerification(t *testing.T) {
	e := suppliedAll(t)
	_, out, _ := icall(e, "GET", "/api/market/indices", nil)
	for id, want := range map[string]int{"index_sp500": 500, "index_nasdaq100": 100, "index_omxs30": 30} {
		ix := indexByID(t, out, id)
		if ix["available"] != true || int(ix["count"].(float64)) != want {
			t.Errorf("%s: available=%v count=%v", id, ix["available"], ix["count"])
		}
		v, _ := ix["verification"].(map[string]any)
		if v == nil || v["level"] != "verified" || v["identity_share"].(float64) != 1 {
			t.Errorf("%s: verification %v", id, ix["verification"])
		}
	}
	adapters := out["adapters"].([]any)
	if len(adapters) < 2 {
		t.Fatalf("adapters: %v", adapters)
	}
	for _, a := range adapters {
		if !strings.Contains(a.(map[string]any)["restrictions"].(string), "does not by itself give you the right") {
			t.Errorf("adapter %v lacks the licence notice", a)
		}
	}
	// The payload never carries the list itself.
	if cons := indexByID(t, out, "index_sp500")["constituents"].([]any); len(cons) != 0 {
		t.Errorf("the list must stay out of the payload, got %d rows", len(cons))
	}
}

func TestAnOutdatedOrOffSizeListIsApproximateNotVerified(t *testing.T) {
	e, _ := indexTestEngine(t)
	body := csvBody(fixture.CSV(fixture.Rows("NDB", "", 88, 5000, "US")))
	if code, _, _ := icall(e, "PUT", "/api/market/indices/index_nasdaq100/csv", body); code != 200 {
		t.Fatal("import refused")
	}
	body = csvBody(fixture.CSV(fixture.Rows("OMB", ".ST", 30, 9000, "SE")))
	body["as_of"] = time.Now().UTC().AddDate(0, 0, -200).Format("2006-01-02")
	if code, _, _ := icall(e, "PUT", "/api/market/indices/index_omxs30/csv", body); code != 200 {
		t.Fatal("import refused")
	}
	_, out, _ := icall(e, "GET", "/api/market/indices", nil)
	for _, id := range []string{"index_nasdaq100", "index_omxs30"} {
		v := indexByID(t, out, id)["verification"].(map[string]any)
		if v["level"] != "approximate" {
			t.Errorf("%s must be approximate: %v", id, v)
		}
	}
}

func TestCompareMatchesByIdentityAndDisclosesDifferences(t *testing.T) {
	e, _ := indexTestEngine(t)
	// 20 securities are the same companies in both lists (same ISIN) but carry a different ticker in the Nasdaq list.
	sp := fixture.Rows("SPA", "", 500, 1000, "US")
	nd := fixture.Rows("NDA", "", 100, 5000, "US")
	for i := 0; i < 20; i++ {
		nd[i].ISIN = sp[i].ISIN
	}
	// A ticker collision: the same ticker names different companies (different ISINs); it must not count as shared.
	nd[50].Symbol = sp[200].Symbol
	importIndex(t, e, "index_sp500", sp, "")
	importIndex(t, e, "index_nasdaq100", nd, "")
	importIndex(t, e, "index_omxs30", fixture.Rows("OMA", ".ST", 30, 9000, "SE"), "")

	code, out, raw := icall(e, "GET", "/api/market/indices/compare?ids=index_sp500,index_nasdaq100,index_omxs30", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	cmp := out["comparison"].(map[string]any)
	pairs := cmp["pairs"].([]any)
	if len(pairs) != 3 {
		t.Fatalf("3 indices give 3 pairs, got %d", len(pairs))
	}
	var first map[string]any
	for _, p := range pairs {
		if pm := p.(map[string]any); pm["a"] == "index_sp500" && pm["b"] == "index_nasdaq100" {
			first = pm
		}
	}
	if int(first["shared"].(float64)) != 20 {
		t.Errorf("shared by ISIN: %v (the collision on %s must not count)", first["shared"], sp[200].Symbol)
	}
	if int(cmp["union_count"].(float64)) != 500+100+30-20 {
		t.Errorf("union %v", cmp["union_count"])
	}
	warnings := fmt.Sprint(cmp["warnings"])
	for _, want := range []string{"different currencies (SEK, USD)", "price indices", "survivorship-biased"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("missing warning %q in %s", want, warnings)
		}
	}
	if !strings.Contains(cmp["matching"].(string), "never a match") {
		t.Errorf("matching rule not stated: %v", cmp["matching"])
	}
}

func TestCompareNamesUnavailableIndicesAndValidatesInput(t *testing.T) {
	e, _ := indexTestEngine(t)
	importIndex(t, e, "index_omxs30", fixture.Rows("OMA", ".ST", 30, 9000, "SE"), "")
	code, out, _ := icall(e, "GET", "/api/market/indices/compare?ids=index_omxs30,index_sp500", nil)
	if code != 200 {
		t.Fatalf("%d", code)
	}
	cmp := out["comparison"].(map[string]any)
	if fmt.Sprint(cmp["unavailable"]) != "[index_sp500]" || len(cmp["indices"].([]any)) != 1 || len(cmp["pairs"].([]any)) != 0 {
		t.Errorf("an index without a list must be named, not estimated: %v", cmp)
	}
	for _, q := range []string{"ids=index_sp500", "ids=index_sp500,index_sp500", "ids=index_sp500,index_nope", ""} {
		if code, _, _ := icall(e, "GET", "/api/market/indices/compare?"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%q: want 400, got %d", q, code)
		}
	}
}

func TestHistoryFileGivesPointInTimeMembership(t *testing.T) {
	e, _ := indexTestEngine(t)
	rows := fixture.Rows("NDA", "", 100, 5000, "US")
	history := fixture.History([][3]string{{"2026-03-02", "NDA001", "OLD001"}, {"2025-08-04", "NDA002", ""}})
	importIndex(t, e, "index_nasdaq100", rows, history)
	_, out, _ := icall(e, "GET", "/api/market/indices", nil)
	ix := indexByID(t, out, "index_nasdaq100")
	src := ix["source"].(map[string]any)
	if src["point_in_time"] != true || src["history_from"] != "2025-08-04" || int(src["change_records"].(float64)) != 2 {
		t.Errorf("history not applied: %v", src)
	}
	hist := ix["history"].(map[string]any)
	if hist["point_in_time"] != true {
		t.Errorf("view history: %v", hist)
	}
	if strings.Contains(fmt.Sprint(ix["warnings"]), "survivorship") {
		t.Errorf("survivorship warning should be gone once history is supplied: %v", ix["warnings"])
	}
	// A bad history file is refused and nothing is saved.
	e2, _ := indexTestEngine(t)
	body := csvBody(fixture.CSV(rows))
	body["history_csv"] = fixture.History([][3]string{{"2999-01-01", "NDA001", ""}})
	if code, _, _ := icall(e2, "PUT", "/api/market/indices/index_nasdaq100/csv", body); code != http.StatusUnprocessableEntity {
		t.Errorf("a future-dated history must be refused, got %d", code)
	}
	_, out, _ = icall(e2, "GET", "/api/market/indices", nil)
	if indexByID(t, out, "index_nasdaq100")["available"] != false {
		t.Error("a refused import must not activate the index")
	}
}

func TestCombinedUniverseNeedsEveryListAndDeduplicatesByIdentity(t *testing.T) {
	e, _ := indexTestEngine(t)
	sp := fixture.Rows("SPA", "", 500, 1000, "US")
	nd := fixture.Rows("NDA", "", 100, 5000, "US")
	for i := 0; i < 20; i++ {
		nd[i].ISIN = sp[i].ISIN
	}
	importIndex(t, e, "index_sp500", sp, "")
	importIndex(t, e, "index_nasdaq100", nd, "")

	// OMXS30 has no list: the request is refused and names it; nothing is substituted.
	code, out, _ := icall(e, "GET", "/api/market/leaderboard?universe=index_combined&indices=index_sp500,index_omxs30", nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(fmt.Sprint(out["error"]), "OMX Stockholm 30") {
		t.Fatalf("expected a refusal naming OMXS30: %d %v", code, out)
	}
	if code, _, _ := icall(e, "GET", "/api/market/leaderboard?universe=index_combined&indices=index_sp500", nil); code != http.StatusBadRequest {
		t.Errorf("one index is not a combination: %d", code)
	}

	u, status, msg := e.combinedUniverse(t.Context(), 0, []string{"index_sp500", "index_nasdaq100"})
	if status != 0 {
		t.Fatalf("%d %s", status, msg)
	}
	if len(u.Symbols) != 500+100-20 {
		t.Errorf("union size %d, want %d", len(u.Symbols), 580)
	}
	if !strings.Contains(u.Source.Limitations, "20 securities are in more than one") || !strings.Contains(u.Source.Limitations, "backtest takes one index") {
		t.Errorf("limitations: %s", u.Source.Limitations)
	}
	// A combined universe cannot be replayed: refused with the reason.
	code, out, _ = icall(e, "GET", "/api/market/backtest?universe=index_combined&indices=index_sp500,index_nasdaq100", nil)
	if code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(out["error"]), "one index at a time") {
		t.Errorf("combined backtest: %d %v", code, out)
	}
}

func TestSwitchingIndicesChangesTheListSourceAndHistoryTogether(t *testing.T) {
	e := suppliedAll(t)
	_, out, _ := icall(e, "GET", "/api/market/indices", nil)
	sp, se := indexByID(t, out, "index_sp500"), indexByID(t, out, "index_omxs30")
	if sp["currency"] == se["currency"] || sp["benchmark_symbol"] == se["benchmark_symbol"] {
		t.Fatal("the two definitions must differ in currency and benchmark")
	}
	for _, ix := range []map[string]any{sp, se} {
		src := ix["source"].(map[string]any)
		if src["provider"] != "csv" || src["as_of"] == "" || ix["as_of"] != src["as_of"] {
			t.Errorf("%v: snapshot and as-of disagree: %v", ix["id"], src)
		}
	}
	if !strings.Contains(sp["constituent_source"].(map[string]any)["name"].(string), "supplied by you") {
		t.Errorf("provenance must say the list is the user's: %v", sp["constituent_source"])
	}
}
