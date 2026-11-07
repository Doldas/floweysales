package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/market"
	"floweysales/internal/provenance"
	"floweysales/internal/sec"
	"floweysales/internal/sources"
	"floweysales/internal/storage"
	"floweysales/internal/valuation"
)

var provNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// mustValid fails when a fact is inconsistent. Unknown is valid: it is the explicit statement.
func mustValid(t *testing.T, name string, f provenance.Fact) {
	t.Helper()
	if err := f.Validate(); err != nil {
		t.Errorf("%s: %v (%+v)", name, err, f)
	}
	if f.Status == "" || f.Kind == "" {
		t.Errorf("%s: kind and status are required: %+v", name, f)
	}
}

func TestWithProvenanceAddsASidecarWithoutChangingTheData(t *testing.T) {
	type payload struct {
		Symbol string  `json:"symbol"`
		Price  float64 `json:"price"`
	}
	out := withProvenance(payload{"AAPL", 190.5}, provenance.Unspecified("n/a"))
	raw, _ := json.Marshal(out)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["symbol"]) != `"AAPL"` || string(got["price"]) != `190.5` {
		t.Fatalf("data changed: %s", raw)
	}
	var f provenance.Fact
	if err := json.Unmarshal(got["provenance"], &f); err != nil || f.Status != provenance.Unknown {
		t.Fatalf("provenance = %s (%v)", got["provenance"], err)
	}
	// An existing field is kept, and a non-object is returned as is.
	kept := withProvenance(map[string]any{"provenance": "mine"}, provenance.Unspecified("x"))
	if m := kept.(map[string]any); m["provenance"] != "mine" {
		t.Fatalf("existing provenance replaced: %v", m)
	}
	if arr := withProvenance([]int{1}, provenance.Unspecified("x")); len(arr.([]int)) != 1 {
		t.Fatal("array changed")
	}
}

func TestQuoteHistoryAndFinancialsNameProviderAndDates(t *testing.T) {
	snap := market.QuoteSnapshot{Symbol: "AAPL", Source: "YAHOO", Price: 190, AsOfUnix: 1_762_500_000, FetchedAtUnix: 1_762_500_060, Stale: true}
	q := quoteProvenance(snap)
	mustValid(t, "quote", q)
	if q.Provider != "YAHOO" || q.AsOf == nil || q.RetrievedAt == nil || q.Status != provenance.Known || !strings.Contains(q.Note, "older") {
		t.Fatalf("quote fact = %+v", q)
	}
	if q.AsOf.Location() != time.UTC {
		t.Fatal("as-of is not UTC")
	}

	h := historyProvenance(market.HistorySeries{Symbol: "AAPL", Provider: "YAHOO", Points: []market.HistoryPoint{{Timestamp: 1_762_400_000}, {Timestamp: 1_762_500_000}}}, provNow)
	mustValid(t, "history", h)
	if h.AsOf == nil || h.AsOf.Unix() != 1_762_500_000 {
		t.Fatalf("history as-of should be the last bar: %+v", h.AsOf)
	}

	f := financialsProvenance(market.FinancialsResponse{Symbol: "AAPL", Provider: "YAHOO", Source: "yahoo",
		Reports: []market.QuarterlyFinancial{{PeriodEnd: "2026-03-31"}, {PeriodEnd: "2026-06-30"}}}, provNow)
	mustValid(t, "financials", f)
	if f.AsOf == nil || f.AsOf.Format("2006-01-02") != "2026-06-30" {
		t.Fatalf("financials as-of = %v", f.AsOf)
	}
}

func TestMissingSourceIsStatedNeverFilledIn(t *testing.T) {
	// No provider named anywhere: the fact is unknown and says so.
	for name, f := range map[string]provenance.Fact{
		"quote":      quoteProvenance(market.QuoteSnapshot{Symbol: "X"}),
		"history":    historyProvenance(market.HistorySeries{Symbol: "X"}, provNow),
		"financials": financialsProvenance(market.FinancialsResponse{Symbol: "X", Source: "none"}, provNow),
		"tick":       recordProvenance(storage.TradingRecord{Symbol: "X", Timestamp: 100}),
	} {
		mustValid(t, name, f)
		if f.Status != provenance.Unknown || f.Note == "" {
			t.Errorf("%s without a provider must be unknown with a note: %+v", name, f)
		}
	}
	// A provider with no bars or reports is partial, not known.
	if f := historyProvenance(market.HistorySeries{Symbol: "X", Provider: "YAHOO"}, provNow); f.Status != provenance.Partial {
		t.Errorf("empty history = %+v", f)
	}
	if f := financialsProvenance(market.FinancialsResponse{Symbol: "X", Provider: "YAHOO", Note: "crypto"}, provNow); f.Status != provenance.Partial || !strings.Contains(f.Note, "crypto") {
		t.Errorf("empty financials = %+v", f)
	}
}

func TestTickProvenanceUsesTheProvidersQuoteTimeOnlyWhenGiven(t *testing.T) {
	withQuote := recordProvenance(storage.TradingRecord{Symbol: "AAPL", Provider: "YAHOO", Timestamp: 2000, QuoteTime: 1900})
	mustValid(t, "tick", withQuote)
	if withQuote.AsOf == nil || withQuote.AsOf.Unix() != 1900 || withQuote.RetrievedAt.Unix() != 2000 {
		t.Fatalf("with quote time: %+v", withQuote)
	}
	without := recordProvenance(storage.TradingRecord{Symbol: "AAPL", Provider: "YAHOO", Timestamp: 2000})
	if without.AsOf != nil || !strings.Contains(without.Note, "no quote time") {
		t.Fatalf("a missing quote time must not be invented: %+v", without)
	}
	if p := tickPayload(storage.TradingRecord{Symbol: "AAPL", Provider: "YAHOO", Timestamp: 2000}); p["provenance"] == nil {
		t.Fatal("the stream tick carries no provenance")
	}
}

func TestSECSurfacesPointAtEDGAR(t *testing.T) {
	filings := secFilingsProvenance(sec.Filings{Source: "SEC EDGAR submissions", RetrievedAt: provNow,
		Items: []sec.Filing{{Form: "10-K", DocumentURL: "https://www.sec.gov/Archives/x.htm"}}})
	mustValid(t, "filings", filings)
	if filings.URL != "https://www.sec.gov/Archives/x.htm" || filings.Official == nil || !*filings.Official || filings.Status != provenance.Known {
		t.Fatalf("filings = %+v", filings)
	}
	facts := secFactsProvenance(sec.Facts{RetrievedAt: provNow, Values: []sec.Fact{{End: "2026-06-30", FilingURL: "https://www.sec.gov/y"}, {End: "2026-03-31"}}})
	mustValid(t, "facts", facts)
	if facts.AsOf == nil || facts.AsOf.Format("2006-01-02") != "2026-06-30" {
		t.Fatalf("facts as-of = %v", facts.AsOf)
	}
	section := secSectionProvenance(sec.Section{Accession: "0000320193-26-000001", Filed: "2026-05-01", DocumentURL: "https://www.sec.gov/z", Truncated: true, RetrievedAt: provNow})
	mustValid(t, "section", section)
	if section.DocumentID == "" || section.PublishedAt == nil || !strings.Contains(section.Note, "truncated") {
		t.Fatalf("section = %+v", section)
	}
}

func TestComputedResultsNameTheirMethod(t *testing.T) {
	result := map[string]any{"provider": "YAHOO", "generated_at": "2026-11-07T12:00:00Z", "data_end": "2026-11-06"}
	for _, kind := range []string{"single", "batch", "montecarlo", "sensitivity"} {
		f := liveBacktestProvenance(kind, result)
		mustValid(t, "backtest "+kind, f)
		if f.Kind != provenance.Calculated || f.Method == nil || f.Method.ID != "backtest."+kind || f.Method.Formula == "" || f.Status != provenance.Known {
			t.Errorf("backtest %s = %+v", kind, f)
		}
	}
	for _, kind := range []string{"backtest", "walkforward", "montecarlo"} {
		f := strategyResultProvenance(kind, result)
		mustValid(t, "strategy "+kind, f)
		if f.Method == nil || f.Method.ID != "strategy."+kind || f.AsOf == nil {
			t.Errorf("strategy %s = %+v", kind, f)
		}
	}
	// A result that records no provider is partial; an unknown kind claims nothing.
	if f := strategyResultProvenance("backtest", map[string]any{"generated_at": "2026-11-07T12:00:00Z"}); f.Status != provenance.Partial {
		t.Errorf("no provider = %+v", f)
	}
	if f := strategyResultProvenance("nope", result); f.Status != provenance.Unknown {
		t.Errorf("unknown kind = %+v", f)
	}
	if f := liveBacktestProvenance("nope", result); f.Status != provenance.Unknown {
		t.Errorf("unknown kind = %+v", f)
	}
}

func TestDCFAndOtherCalculationsSayWhichInputsAreAssumptions(t *testing.T) {
	d := dcfProvenance(valuation.Output{PeriodEnd: "2026-06-30", SourceURLs: []string{"https://www.sec.gov/f"}, Caution: "Not advice."}, provNow)
	mustValid(t, "dcf", d)
	if d.Status != provenance.Known || len(d.Inputs) != 1 || !strings.Contains(d.Note, "assumptions") || d.AsOf == nil {
		t.Fatalf("dcf = %+v", d)
	}
	if d := dcfProvenance(valuation.Output{}, provNow); d.Status != provenance.Partial {
		t.Errorf("dcf without sources should be partial: %+v", d)
	}
	for name, f := range map[string]provenance.Fact{
		"portfolio": portfolioAnalyticsProvenance("YAHOO", provNow),
		"stats":     statsProvenance("AAPL", provNow),
		"positions": positionsProvenance(provNow),
		"plan":      planProvenance(provNow),
		"sizing":    sizingProvenance("AAPL", "atr", provNow),
		"indices":   indexRegistryProvenance(provNow),
		"summary":   summaryProvenance(7, provNow),
		"plugin":    pluginProvenance("acme", "quote", provNow),
		"research":  researchProvenance(market.ResearchResult{Provider: "YAHOO", Operation: "profile", Symbol: "AAPL", RetrievedAt: "2026-11-07T12:00:00Z"}, provNow),
	} {
		mustValid(t, name, f)
		if f.Status == provenance.Unknown {
			t.Errorf("%s should state its origin: %+v", name, f)
		}
	}
	if f := portfolioAnalyticsProvenance("", provNow); f.Status != provenance.Partial {
		t.Errorf("portfolio without a provider = %+v", f)
	}
	if f := summaryProvenance(7, provNow); f.Kind != provenance.AIGenerated {
		t.Errorf("an AI summary must say so: %+v", f)
	}
}

func TestTodayBriefingHasAFactForEveryPart(t *testing.T) {
	body := map[string]any{
		"phase": map[string]any{}, "portfolio": map[string]any{}, "macro": map[string]any{},
		"worth_a_look": map[string]any{"leaderboard": 1, "insider": 1, "earnings": 1},
	}
	facts := todayProvenance(body, provNow)
	want := []string{"phase", "portfolio", "macro", "worth_a_look.leaderboard", "worth_a_look.insider", "worth_a_look.earnings"}
	if len(facts) != len(want) {
		t.Fatalf("facts = %v", facts)
	}
	for _, key := range want {
		f, ok := facts[key]
		if !ok {
			t.Errorf("no fact for %s", key)
			continue
		}
		mustValid(t, key, f)
		if f.Status == provenance.Unknown {
			t.Errorf("%s is unknown", key)
		}
	}
	if f := sectionProvenance("something_new", provNow); f.Status != provenance.Unknown {
		t.Errorf("a part nobody described must say its source is unknown: %+v", f)
	}
}

// Every investor node type must be classified, so a new node cannot return
// data without saying where it came from.
func TestEveryInvestorNodeTypeIsClassifiedForProvenance(t *testing.T) {
	for typ := range investorNodeTypes {
		if class := flowNodeProvenance[typ]; class != "stamped" && class != "passthrough" {
			t.Errorf("investor node %q has no provenance class in flowNodeProvenance", typ)
		}
	}
	for typ := range flowNodeProvenance {
		if !investorNodeTypes[typ] {
			t.Errorf("flowNodeProvenance lists %q, which is not an investor node", typ)
		}
	}
}

func TestStampedNodesGiveEveryRowAValidFact(t *testing.T) {
	row := func() map[string]any {
		return map[string]any{"symbol": "AAPL", "as_of": "2026-11-06", "url": "https://example.com/a", "form": "8-K", "filed": "2026-11-05",
			"published": "2026-11-06T09:00:00Z", "date": "2026-11-20", "provider": "YAHOO", "ttm_through": "2026-06-30"}
	}
	for typ, class := range flowNodeProvenance {
		if class != "stamped" {
			continue
		}
		n := flowNode{Type: typ, Strategy: "quality", Universe: "us_large"}
		in, _ := json.Marshal([]map[string]any{row(), {"symbol": "ERR", "error": "no data"}})
		out := stampFlowProvenance(n, "YAHOO", string(in), provNow)
		var rows []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 {
			t.Fatalf("%s: output %q (%v)", typ, out, err)
		}
		var f provenance.Fact
		if err := json.Unmarshal(rows[0]["provenance"], &f); err != nil {
			t.Fatalf("%s: row has no provenance: %s", typ, out)
		}
		mustValid(t, typ, f)
		if f.Status == provenance.Unknown {
			t.Errorf("%s: a row with a known provider is unknown: %+v", typ, f)
		}
		if _, has := rows[1]["provenance"]; has {
			t.Errorf("%s: an error row is not data and gets no fact", typ)
		}
		if !strings.Contains(string(rows[0]["symbol"]), "AAPL") {
			t.Errorf("%s: data changed", typ)
		}
	}
}

func TestFlowNewsRowsNeverInventAPublicationTime(t *testing.T) {
	n := flowNode{Type: "news"}
	out := stampFlowProvenance(n, "YAHOO", `[{"symbol":"AAPL","title":"t","url":"https://example.com/n","date_status":"unavailable"}]`, provNow)
	var rows []struct {
		Provenance provenance.Fact `json:"provenance"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	f := rows[0].Provenance
	mustValid(t, "news", f)
	if f.PublishedAt != nil || f.URL != "https://example.com/n" || f.RetrievedAt == nil || f.Status != provenance.Known {
		t.Fatalf("news fact = %+v", f)
	}
	// A row without a provider or link is stated as unknown rather than attributed.
	bare := stampFlowProvenance(n, "", `[{"symbol":"AAPL","title":"t"}]`, provNow)
	var rows2 []struct {
		Provenance provenance.Fact `json:"provenance"`
	}
	_ = json.Unmarshal([]byte(bare), &rows2)
	if rows2[0].Provenance.Status != provenance.Unknown {
		t.Fatalf("a row with no origin must be unknown: %+v", rows2[0].Provenance)
	}
}

func TestStampingLeavesOtherOutputAndExistingFactsAlone(t *testing.T) {
	for name, tc := range map[string]struct {
		node flowNode
		out  string
	}{
		"pass-through node": {flowNode{Type: "screen"}, `[{"symbol":"AAPL"}]`},
		"text":              {flowNode{Type: "news"}, "No rows."},
		"object":            {flowNode{Type: "news"}, `{"a":1}`},
		"empty":             {flowNode{Type: "news"}, ""},
	} {
		if got := stampFlowProvenance(tc.node, "YAHOO", tc.out, provNow); got != tc.out {
			t.Errorf("%s: changed to %q", name, got)
		}
	}
	kept := stampFlowProvenance(flowNode{Type: "news"}, "YAHOO", `[{"symbol":"A","provenance":{"kind":"observed","status":"known","provider":"mine"}}]`, provNow)
	if !strings.Contains(kept, `"mine"`) || strings.Count(kept, "provenance") != 1 {
		t.Fatalf("an existing fact was replaced: %s", kept)
	}
}

func TestReportTableListsSourcesAndStatesUnknownOnes(t *testing.T) {
	rows := `[{"symbol":"AAPL","price":190,"provenance":{"kind":"observed","status":"known","provider":"YAHOO"}},{"symbol":"MSFT","price":400}]`
	out, err := reportTable(rows, "T", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "_Sources: YAHOO (observed); source unknown for 1 row(s)") {
		t.Fatalf("no sources line:\n%s", out)
	}
	if strings.Contains(strings.Split(out, "\n")[2], "provenance") {
		t.Fatalf("the nested fact must not become a column:\n%s", out)
	}
}

func TestEveryAgentToolSaysWhereItsDataComesFrom(t *testing.T) {
	for _, name := range ai.ToolNames() {
		if _, ok := sources.ToolOrigins[name]; !ok {
			t.Errorf("agent tool %q has no entry in sources.ToolOrigins", name)
		}
	}
	for name := range agentToolClass {
		if _, ok := sources.ToolOrigins[name]; !ok {
			t.Errorf("agent tool %q (agentToolClass) has no entry in sources.ToolOrigins", name)
		}
	}
	for name := range sources.ToolOrigins {
		if _, ok := agentToolClass[name]; !ok {
			t.Errorf("sources.ToolOrigins lists %q, which is not an agent tool", name)
		}
	}
}
