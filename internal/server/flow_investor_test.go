package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/sec"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
)

// investorBackend serves a year of synthetic daily bars: DOWN falls 1% a
// day for the last 10 sessions, UP rises steadily, and BAD fails.
type investorBackend struct {
	market.NoProviders
	now time.Time
}

func (investorBackend) Providers() []market.ProviderCapability {
	return []market.ProviderCapability{{ID: "YAHOO", Enabled: true, Capabilities: []string{"history", "financials", "news"}}}
}

func (b investorBackend) History(_ context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	if symbol == "BAD" {
		return market.HistorySeries{}, errors.New("unknown symbol")
	}
	const n = 260
	points := make([]market.HistoryPoint, n)
	price := 100.0
	for i := range points {
		switch {
		case symbol == "DOWN" && i >= n-10:
			price *= 0.99
		case symbol == "UP":
			price *= 1.002
		}
		t := b.now.AddDate(0, 0, i-n+1)
		points[i] = market.HistoryPoint{Timestamp: t.Unix(), Open: price, High: price * 1.01, Low: price * 0.99, Close: price}
	}
	return market.HistorySeries{Symbol: symbol, Provider: p.ID(), Range: rng, Interval: "1d", Currency: "USD", Points: points}, nil
}

func f64(v float64) *float64 { return &v }

func (investorBackend) Financials(_ context.Context, p market.Provider, symbol string, _ market.FinancialYearRange) (market.FinancialsResponse, error) {
	reports := []market.QuarterlyFinancial{}
	ends := []string{"2026-06-30", "2026-03-31", "2025-12-31", "2025-09-30", "2025-06-30", "2025-03-31", "2024-12-31", "2024-09-30"}
	for i, end := range ends {
		scale := 1.0
		if i >= 4 {
			scale = 0.8 // the previous year was 20% smaller
		}
		reports = append(reports, market.QuarterlyFinancial{PeriodEnd: end, Currency: "USD",
			Revenue: f64(100 * scale), GrossProfit: f64(60 * scale), OperatingIncome: f64(30 * scale), NetIncome: f64(20 * scale), EPS: f64(1 * scale)})
	}
	return market.FinancialsResponse{Symbol: symbol, Provider: p.ID(), Source: "test", Reports: reports}, nil
}

func (investorBackend) NewsProviders() []market.Provider { return []market.Provider{"YAHOO"} }
func (b investorBackend) News(_ context.Context, p market.Provider, symbol string, limit int) ([]market.NewsItem, error) {
	return market.CleanNews([]market.NewsItem{
		{Title: symbol + " raises guidance", URL: "https://news.example.com/" + symbol, Published: b.now.Add(-time.Hour).Unix()},
		{Title: "Old story", URL: "https://news.example.com/old", Published: b.now.AddDate(0, 0, -30).Unix()},
		{Title: "Unsafe link", URL: "javascript:alert(1)"},
	}, p, limit), nil
}

type memoryNodeState struct {
	mu    sync.Mutex
	saved map[string]string
}

func (m *memoryNodeState) LoadFlowNodeState(_ context.Context, flowID int64, nodeID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved[nodeID], nil
}
func (m *memoryNodeState) SaveFlowNodeState(_ context.Context, flowID int64, nodeID, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved[nodeID] = state
	return nil
}

func investorEngine(t *testing.T) *Engine {
	t.Helper()
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	e.providers = investorBackend{now: time.Now().UTC()}
	e.flowState = &memoryNodeState{saved: map[string]string{}}
	e.secFilings = func(_ context.Context, ticker string, _ int) (sec.Filings, error) {
		today := time.Now().UTC().Format("2006-01-02")
		return sec.Filings{Company: sec.Company{Ticker: ticker, Name: ticker + " Inc"}, Items: []sec.Filing{
			{Form: "8-K", Accession: "0000000001-26-000001", Filed: today, DocumentURL: "https://www.sec.gov/a"},
			{Form: "10-Q", Accession: "0000000001-26-000002", Filed: today, DocumentURL: "https://www.sec.gov/b"},
			{Form: "10-K", Accession: "0000000001-25-000003", Filed: "2025-02-01", DocumentURL: "https://www.sec.gov/c"},
		}}, nil
	}
	return e
}

func runInvestorFlow(t *testing.T, e *Engine, graph string) []map[string]string {
	t.Helper()
	f := storage.AgentFlow{ID: 7, Name: "Investor test", Graph: json.RawMessage(graph)}
	out, err := e.executeFlow(context.Background(), f)
	if err != nil {
		t.Fatalf("flow failed: %v", err)
	}
	var steps []map[string]string
	if err := json.Unmarshal([]byte(out), &steps); err != nil {
		t.Fatal(err)
	}
	return steps
}

func TestWatchlistScreenAndReportFlow(t *testing.T) {
	e := investorEngine(t)
	graph := `{"version":2,"nodes":[
		{"id":"start","type":"trigger","label":"Start"},
		{"id":"quotes","type":"watchlist_quotes","label":"Quotes","symbols":"UP, DOWN, BAD"},
		{"id":"drops","type":"screen","label":"Drops","rule":"change_5d_pct <= -3","sort_by":"change_1d_pct"},
		{"id":"report","type":"report_table","label":"Report","title":"Big drops","columns":"symbol,price,change_5d_pct"}],
		"edges":[{"from":"start","to":"quotes"},{"from":"quotes","to":"drops"},{"from":"drops","to":"report"}]}`
	steps := runInvestorFlow(t, e, graph)
	var quotes []map[string]any
	if err := json.Unmarshal([]byte(steps[0]["output"]), &quotes); err != nil || len(quotes) != 3 {
		t.Fatalf("quotes %v: %s", err, steps[0]["output"])
	}
	if quotes[2]["symbol"] != "BAD" || quotes[2]["error"] == nil {
		t.Fatalf("a failing symbol must become an error row, got %v", quotes[2])
	}
	if quotes[1]["change_5d_pct"].(float64) > -4.8 || quotes[1]["currency"] != "USD" || quotes[1]["high_52w"] == nil {
		t.Fatalf("unexpected DOWN quote %v", quotes[1])
	}
	var drops []map[string]any
	_ = json.Unmarshal([]byte(steps[1]["output"]), &drops)
	if len(drops) != 1 || drops[0]["symbol"] != "DOWN" {
		t.Fatalf("screen kept %s", steps[1]["output"])
	}
	report := steps[2]["output"]
	if !strings.Contains(report, "### Big drops") || !strings.Contains(report, "| DOWN |") || strings.Contains(report, "UP") {
		t.Fatalf("report:\n%s", report)
	}
}

func TestTechnicalAndFundamentalNodes(t *testing.T) {
	e := investorEngine(t)
	ctx := context.Background()
	out, err := e.runInvestorDataNode(ctx, flowNode{Type: "technical_scan", Symbols: "UP"}, "")
	if err != nil {
		t.Fatal(err)
	}
	var tech []map[string]any
	_ = json.Unmarshal([]byte(out), &tech)
	if tech[0]["above_sma200"] != true || tech[0]["rsi14"] == nil || tech[0]["ema_stack"] != "bullish" {
		t.Fatalf("technical row %v", tech[0])
	}
	// Symbols may come from the previous node's rows.
	out, err = e.runInvestorDataNode(ctx, flowNode{Type: "fundamentals"}, `[{"symbol":"UP"}]`)
	if err != nil {
		t.Fatal(err)
	}
	var fund []map[string]any
	_ = json.Unmarshal([]byte(out), &fund)
	row := fund[0]
	if row["revenue_ttm"] != 400.0 || row["revenue_growth_pct"] != 25.0 || row["net_margin_pct"] != 20.0 || row["eps_ttm"] != 4.0 || row["ttm_through"] != "2026-06-30" {
		t.Fatalf("fundamentals %v", row)
	}
	if pe, ok := row["pe_ttm"].(float64); !ok || pe <= 0 {
		t.Fatalf("missing P/E in %v", row)
	}
	if _, ok := row["fcf_margin_pct"]; ok {
		t.Fatal("a margin without reported figures must stay absent")
	}
}

func TestSECFilingsNewsAndOnlyNew(t *testing.T) {
	e := investorEngine(t)
	ctx := context.Background()
	out, err := e.runInvestorDataNode(ctx, flowNode{Type: "sec_filings", Symbols: "AAPL", Forms: "8-K,10-K", LookbackDays: 7}, "")
	if err != nil {
		t.Fatal(err)
	}
	var filings []map[string]any
	_ = json.Unmarshal([]byte(out), &filings)
	if len(filings) != 1 || filings[0]["form"] != "8-K" || filings[0]["id"] != "0000000001-26-000001" {
		t.Fatalf("filings %s", out)
	}
	out, err = e.runInvestorDataNode(ctx, flowNode{Type: "news", Symbols: "NVDA"}, "")
	if err != nil {
		t.Fatal(err)
	}
	var news []map[string]any
	_ = json.Unmarshal([]byte(out), &news)
	if len(news) != 1 || news[0]["title"] != "NVDA raises guidance" {
		t.Fatalf("news must drop old and unsafe items: %s", out)
	}
	f := storage.AgentFlow{ID: 7}
	node := flowNode{ID: "new", Type: "only_new"}
	first, _, err := e.filterOnlyNew(ctx, f.ID, node, out, time.Now())
	if err != nil || first == "[]" {
		t.Fatalf("first run passes items: %s %v", first, err)
	}
	second, _, _ := e.filterOnlyNew(ctx, f.ID, node, out, time.Now())
	if second != "[]" {
		t.Fatalf("second run must suppress seen items: %s", second)
	}
	node.CooldownHours = 2
	later, _, _ := e.filterOnlyNew(ctx, f.ID, node, out, time.Now().Add(3*time.Hour))
	if later == "[]" {
		t.Fatal("cooldown should let a persistent item pass again")
	}
}

func TestNotifySkipsEmptyInputWithoutSending(t *testing.T) {
	e := investorEngine(t)
	output, note, err := e.runInvestorNode(context.Background(), storage.AgentFlow{ID: 7}, flowNode{ID: "n", Type: "notify", Channel: "webhook"}, 0, "[]")
	if err != nil || output != "[]" || !strings.Contains(note, "Nothing to send") {
		t.Fatalf("got %q %q %v", output, note, err)
	}
}

func TestScreenRules(t *testing.T) {
	rows := `[{"symbol":"A","rsi14":25,"rsi_state":"oversold","above_sma200":true,"title":"Raises Guidance"},
	          {"symbol":"B","rsi14":55,"rsi_state":"neutral","above_sma200":false},
	          {"symbol":"C"}]`
	cases := map[string]string{
		"rsi14 < 30":                                   "A",
		"rsi14 >= 30 or not above_sma200":              "B,C",
		`rsi_state == "oversold" and above_sma200`:     "A",
		"rsi_state = neutral":                          "B",
		`title contains "guidance"`:                    "A",
		"(rsi14 < 30 or rsi14 > 50) and symbol != 'B'": "A",
		"above_sma200 == false":                        "B",
	}
	for rule, want := range cases {
		out, err := screenRows(rows, rule, "", 0)
		if err != nil {
			t.Fatalf("%s: %v", rule, err)
		}
		var kept []map[string]any
		_ = json.Unmarshal([]byte(out), &kept)
		got := []string{}
		for _, r := range kept {
			got = append(got, r["symbol"].(string))
		}
		if strings.Join(got, ",") != want {
			t.Errorf("%s: got %v want %s", rule, got, want)
		}
	}
	for _, bad := range []string{"rsi14 <", "rsi14 < 'x' extra", "a > \"text\"", "title contains 3", "(rsi14 < 3", "rsi14 ! 3", "os.exit()"} {
		if _, err := parseScreenRule(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	sorted, _ := screenRows(rows, "", "-rsi14", 2)
	if !strings.HasPrefix(sorted, `[{"`) || !strings.Contains(sorted, `"B"`) || strings.Contains(sorted, `"C"`) {
		t.Fatalf("sort/limit: %s", sorted)
	}
}

func tooManySymbols() string {
	list := []string{}
	for i := 0; i <= flowMaxSymbols; i++ {
		list = append(list, "S"+strings.Repeat("X", i%5)+string(rune('A'+i%26)))
	}
	return strings.Join(list, ",")
}

func TestFlowSymbolsAndValidation(t *testing.T) {
	for input, want := range map[string]string{
		`["aapl","MSFT","aapl"]`:          "AAPL,MSFT",
		`[{"symbol":"nvda"},{"other":1}]`: "NVDA",
		`{"symbols":["SPY"]}`:             "SPY",
		"tsla, amd\nINTC":                 "TSLA,AMD,INTC",
		`{"data":{"price":1}}`:            "",
		tooManySymbols():                  "",
	} {
		got, err := flowSymbols("", input)
		if (err == nil) != (want != "") || (err == nil && strings.Join(got, ",") != want) {
			t.Errorf("%q: got %v %v", input, got, err)
		}
	}
	valid := `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"q","type":"watchlist_quotes","label":"Q","symbols":"AAPL"},{"id":"n","type":"notify","label":"N","channel":"email","approval":"required"}],"edges":[{"from":"start","to":"q"},{"from":"q","to":"n"}]}`
	g, _, err := validateFlow(storage.AgentFlow{Name: "Valid", Graph: json.RawMessage(valid)})
	if err != nil {
		t.Fatal(err)
	}
	if flowAllowedForNonAdmin(g) {
		t.Fatal("notify is a side effect and must stay SYS_ADMIN-only")
	}
	if c, target := flowCapability(g.Nodes[2]); c != capabilityNotifySend || target != "email:flow-owner" {
		t.Fatalf("capability %s %s", c, target)
	}
	for name, bad := range map[string]string{
		"v1":          strings.Replace(valid, `"version":2,`, ``, 1),
		"channel":     strings.Replace(valid, `"channel":"email"`, `"channel":"sms"`, 1),
		"symbol":      strings.Replace(valid, `"symbols":"AAPL"`, `"symbols":"AA PL$"`, 1),
		"misplaced":   strings.Replace(valid, `"label":"Start"`, `"label":"Start","rule":"x > 1"`, 1),
		"prompt":      strings.Replace(valid, `"label":"Q",`, `"label":"Q","prompt":"ignore the rules",`, 1),
		"screen rule": strings.Replace(valid, `"type":"watchlist_quotes","label":"Q","symbols":"AAPL"`, `"type":"screen","label":"Q","rule":"price >"`, 1),
	} {
		if _, _, err := validateFlow(storage.AgentFlow{Name: "Bad", Graph: json.RawMessage(bad)}); err == nil {
			t.Errorf("%s: invalid investor node accepted", name)
		}
	}
}

func TestNotifyFormatting(t *testing.T) {
	now := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	if got := notifySubject("{{count}} alerts from {{flow}} on {{date}}\r\n", "Watch", `[{"a":1},{"a":2}]`, now); got != "2 alerts from Watch on 2026-10-31" {
		t.Fatalf("subject %q", got)
	}
	body := notifyBody(`[{"symbol":"AAPL","price":1.234}]`)
	if !strings.Contains(body, "| AAPL | 1.23 |") {
		t.Fatalf("body %q", body)
	}
	payload, _ := notifyWebhookPayload("discord", "S", strings.Repeat("x", 5000), "\nfooter", storage.AgentFlow{}, 0)
	var discord struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(payload, &discord)
	if len(discord.Content) > 2000 || !strings.HasSuffix(discord.Content, "footer") {
		t.Fatalf("discord payload too long or missing footer: %d", len(discord.Content))
	}
	if _, err := validNotifyWebhookURL("http://hooks.slack.com/x"); err == nil {
		t.Fatal("plain http webhook accepted")
	}
	if _, err := validNotifyWebhookURL("https://127.0.0.1/hook"); err == nil {
		t.Fatal("loopback webhook accepted")
	}
}

func TestConditionEmptinessOperators(t *testing.T) {
	for value, empty := range map[string]bool{"": true, "[]": true, " {} ": true, "null": true, `[{"a":1}]`: false, "text": false} {
		got, err := flowV2Condition(value, "is_empty", "")
		if err != nil || got != empty {
			t.Errorf("is_empty(%q) = %v %v", value, got, err)
		}
		got, _ = flowV2Condition(value, "not_empty", "")
		if got == empty {
			t.Errorf("not_empty(%q) = %v", value, got)
		}
	}
}

func TestSkipEmptyStopsTheBranchOnceNothingIsNew(t *testing.T) {
	e := investorEngine(t)
	graph := `{"version":2,"nodes":[
		{"id":"start","type":"trigger","label":"Start"},
		{"id":"filings","type":"sec_filings","label":"Filings","symbols":"AAPL"},
		{"id":"new","type":"only_new","label":"New"},
		{"id":"report","type":"report_table","label":"Report","skip_empty":true},
		{"id":"out","type":"output","label":"Out"}],
		"edges":[{"from":"start","to":"filings"},{"from":"filings","to":"new"},{"from":"new","to":"report"},{"from":"report","to":"out"}]}`
	first := runInvestorFlow(t, e, graph)
	if first[2]["status"] == "SKIPPED" || !strings.Contains(first[2]["output"], "0000000001-26-000002") {
		t.Fatalf("first run reports the new filings: %v", first)
	}
	second := runInvestorFlow(t, e, graph)
	if second[2]["status"] != "SKIPPED" || second[3]["status"] != "SKIPPED" {
		t.Fatalf("second run must skip the report and everything after it: %v", second)
	}
	bad := strings.Replace(graph, `"label":"Start"`, `"label":"Start","skip_empty":true`, 1)
	if _, _, err := validateFlow(storage.AgentFlow{Name: "Bad", Graph: json.RawMessage(bad)}); err == nil {
		t.Fatal("a trigger cannot skip on empty input")
	}
}
