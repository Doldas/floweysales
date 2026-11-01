package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

func TestWealthPlanNodeReadsTheFlowOwnersPlan(t *testing.T) {
	e, store := planEngine()
	e.flowOwner = func(_ context.Context, flowID int64) (int64, error) { return 42, nil }
	f := storage.AgentFlow{ID: 7}

	out, note, err := e.runWealthPlanNode(context.Background(), f)
	if err != nil || out != "[]" || !strings.Contains(note, "no saved wealth plan") {
		t.Fatalf("empty plan: %q %q %v", out, note, err)
	}

	store.saved[0] = `{"monthly_expenses":9999}` // another user's plan is never read
	store.saved[42] = `{"monthly_expenses":2500,"emergency_fund":9000,"invested":40000,"monthly_contribution":900,"age":34,"retirement_age":64,"risk_profile":"growth","currency":"USD"}`
	out, note, err = e.runWealthPlanNode(context.Background(), f)
	if err != nil || note != "" {
		t.Fatalf("%q %v", note, err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("%v %s", err, out)
	}
	row := rows[0]
	if row["phase"] != "starter" || row["fi_number"] != 750000.0 || row["core_equity_pct"] != 85.0 || row["invested"] != 40000.0 || row["currency"] != "USD" {
		t.Fatalf("%+v", row)
	}
	if _, ok := row["probability_goal_pct"].(float64); !ok || !strings.HasPrefix(row["next_checklist_item"].(string), "Automate") || !strings.Contains(row["projection"].(string), "year 30") {
		t.Fatalf("%+v", row)
	}
	// The same plan and seed give the same figures on a second run.
	out2, _, _ := e.runWealthPlanNode(context.Background(), f)
	if out != out2 {
		t.Fatal("the plan node must be deterministic")
	}
}

func TestWealthPlanNodeIsValidForEveryone(t *testing.T) {
	graph := `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"p","type":"wealth_plan","label":"Plan"}],"edges":[{"from":"start","to":"p"}]}`
	g, _, err := validateFlow(storage.AgentFlow{Name: "Plan check", Graph: json.RawMessage(graph)})
	if err != nil || !flowAllowedForNonAdmin(g) {
		t.Fatalf("%v", err)
	}
}

func TestBacktestNodeRunsOneOrEveryStrategy(t *testing.T) {
	e := backtestEngine(t)
	out, err := e.runBacktestNode(context.Background(), flowNode{Type: "backtest", Symbols: "UP, FLAT", Limit: 1, Strategy: "momentum", Years: 3}, "")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("%v %s", err, out)
	}
	row := rows[0]
	if row["strategy"] != "momentum" || row["universe"] != "custom" || row["cagr_pct"].(float64) <= 0 || row["rebalances"].(float64) < 24 {
		t.Fatalf("%+v", row)
	}
	if !strings.Contains(row["warnings"].(string), "survivorship_bias") || !strings.Contains(row["warning_text"].(string), "today's list of companies") {
		t.Fatalf("the bias warnings travel with the numbers: %+v", row)
	}

	e = backtestEngine(t)
	out, err = e.runBacktestNode(context.Background(), flowNode{Type: "backtest", Symbols: "UP, FLAT", Limit: 1, Strategy: "all"}, "")
	if err != nil {
		t.Fatal(err)
	}
	rows = nil
	_ = json.Unmarshal([]byte(out), &rows)
	if len(rows) != len(leaderboardStrategies) {
		t.Fatalf("one row per strategy: %d", len(rows))
	}
	if len(e.backtestCache) != len(leaderboardStrategies) {
		t.Fatalf("each run is cached: %d", len(e.backtestCache))
	}

	if _, err := e.runBacktestNode(context.Background(), flowNode{Type: "backtest", Symbols: "UP, FLAT", Limit: 5}, ""); err == nil {
		t.Fatal("top above the number of symbols is an error")
	}
}

func TestBacktestNodeValidation(t *testing.T) {
	node := func(extra string) string {
		return `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"b","type":"backtest","label":"Backtest","universe":"us_mega"` + extra + `}],"edges":[{"from":"start","to":"b"}]}`
	}
	valid := storage.AgentFlow{Name: "Backtest it", Graph: json.RawMessage(node(`,"strategy":"all","limit":10,"years":5`))}
	g, _, err := validateFlow(valid)
	if err != nil || !flowAllowedForNonAdmin(g) {
		t.Fatalf("%v", err)
	}
	for name, extra := range map[string]string{
		"strategy": `,"strategy":"yolo"`, "top": `,"limit":26`, "years": `,"years":4`, "universe": `,"universe":"mars"`,
	} {
		raw := node(extra)
		if name == "universe" {
			raw = strings.Replace(node(""), `"us_mega"`, `"mars"`, 1)
		}
		if _, _, err := validateFlow(storage.AgentFlow{Name: "Backtest it", Graph: json.RawMessage(raw)}); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	two := `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"b","type":"backtest","label":"A","universe":"us_mega"},{"id":"c","type":"backtest","label":"B","universe":"us_tech"}],"edges":[{"from":"start","to":"b"},{"from":"b","to":"c"}]}`
	if _, _, err := validateFlow(storage.AgentFlow{Name: "Two backtests", Graph: json.RawMessage(two)}); err == nil || !strings.Contains(err.Error(), "at most one backtest") {
		t.Fatalf("%v", err)
	}
	elsewhere := `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"n","type":"news","label":"N","symbols":"AAPL","years":3}],"edges":[{"from":"start","to":"n"}]}`
	if _, _, err := validateFlow(storage.AgentFlow{Name: "Misplaced", Graph: json.RawMessage(elsewhere)}); err == nil {
		t.Fatal("years belongs to the backtest node")
	}
}
