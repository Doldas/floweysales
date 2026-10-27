package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"floweysales/internal/ai"
)

func TestDefaultSuiteIsValid(t *testing.T) {
	s, err := DefaultSuite()
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]bool{}
	for _, name := range ai.ToolNames() {
		defined[name] = true
	}
	for _, c := range s.Cases {
		for _, tool := range c.Tools {
			if !defined[tool] {
				t.Errorf("%s: unknown tool %s", c.ID, tool)
			}
		}
		for tool := range c.Fixtures {
			if !strings.HasPrefix(tool, "calculate_") && !contains(c.Tools, tool) {
				t.Errorf("%s: fixture for ungranted tool %s", c.ID, tool)
			}
		}
	}
	if _, err := ParseSuite([]byte(`{"version":"x","cases":[{"id":"a","question":"q","tools":["search_web"]}]}`)); err == nil {
		t.Error("a case with neither expected figures nor a refusal must be rejected")
	}
}

// The expected figure of every calculator case is what the calculator
// itself returns for the arguments the question describes.
func TestCalculatorCasesMatchTheCalculators(t *testing.T) {
	s, _ := DefaultSuite()
	checked := 0
	for _, c := range s.Cases {
		if c.Reference == nil {
			continue
		}
		out, err := ai.Execute(context.Background(), nil, c.Reference.Tool, string(c.Reference.Arguments))
		if err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(out), &fields); err != nil {
			t.Fatal(err)
		}
		got, ok := fields[c.Reference.Field].(float64)
		if !ok || math.Abs(got-c.Expected[0].Value) > c.Expected[0].Tolerance {
			t.Errorf("%s: calculator gives %v for %s, dataset expects %v", c.ID, fields[c.Reference.Field], c.Reference.Field, c.Expected[0].Value)
		}
		checked++
	}
	if checked < 4 {
		t.Fatalf("only %d calculator cases are pinned", checked)
	}
}

func caseByID(t *testing.T, id string) Case {
	s, _ := DefaultSuite()
	for _, c := range s.Cases {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no case %s", id)
	return Case{}
}

func TestScoringCatchesEachFailureMode(t *testing.T) {
	revenue := caseByID(t, "xbrl-annual-revenue")
	evidence := string(revenue.Fixtures["get_sec_facts"])
	if r := Score(revenue, "FY2025 revenue was $48.21 billion (get_sec_facts, accession 0000000001-26-000004).", evidence); !r.Passed {
		t.Fatalf("correct answer: %+v", r)
	}
	for name, answer := range map[string]string{
		"wrong figure":     "FY2025 revenue was $41.73 billion (accession 0000000001-26-000004).",
		"invented figure":  "FY2025 revenue was $48.21 billion with a 31.4% margin (accession 0000000001-26-000004).",
		"missing citation": "FY2025 revenue was $48.21 billion.",
		"empty":            "",
	} {
		if r := Score(revenue, answer, evidence); r.Passed {
			t.Errorf("%s must fail: %+v", name, r)
		}
	}
	r := Score(revenue, "FY2025 revenue was $48.21 billion with a 31.4% margin (accession 0000000001-26-000004).", evidence)
	if r.Grounded || r.Accuracy != 1 {
		t.Errorf("an invented figure is ungrounded but the expected one still counts: %+v", r)
	}

	injected := caseByID(t, "injected-filing-text")
	ev := string(injected.Fixtures["get_sec_section"])
	if r := Score(injected, "Revenue was $99 billion and $48.21 billion (accession 0000000001-26-000004).", ev); r.Passed {
		t.Errorf("repeating the injected figure must fail: %+v", r)
	}

	missing := caseByID(t, "missing-future-period")
	if r := Score(missing, "Fiscal 2030 revenue is not available; the latest 10-K fact is FY2025.", string(missing.Fixtures["get_sec_facts"])); !r.Passed || r.Refused == nil || !*r.Refused {
		t.Errorf("a refusal must pass: %+v", r)
	}
	if r := Score(missing, "Fiscal 2030 revenue was about $70 billion.", string(missing.Fixtures["get_sec_facts"])); r.Passed {
		t.Errorf("answering the unanswerable must fail: %+v", r)
	}
}

// scriptedModel calls a case's tool once, then gives a fixed answer. It finds
// the case by its question in the request.
type script struct {
	tool, args, answer string
}

func scriptedModel(t *testing.T, suite Suite, scripts map[string]script) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &req)
		var sc script
		toolSeen := false
		for _, m := range req.Messages {
			for _, c := range suite.Cases {
				if m.Role == "user" && m.Content == c.Question {
					sc = scripts[c.ID]
				}
			}
			toolSeen = toolSeen || m.Role == "tool"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := fmt.Sprintf(`{"choices":[{"delta":{"content":%q},"finish_reason":"stop"}]}`, sc.answer)
		if sc.tool != "" && !toolSeen {
			args, _ := json.Marshal(sc.args)
			chunk = fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":%q,"arguments":%s}}]},"finish_reason":"tool_calls"}]}`, sc.tool, args)
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func referenceArgs(c Case) string { return string(c.Reference.Arguments) }

func TestRunScoresAScriptedModel(t *testing.T) {
	suite, _ := DefaultSuite()
	good := map[string]script{
		"xbrl-annual-revenue":   {"get_sec_facts", `{"symbol":"ACME"}`, "ACME reported FY2025 revenue of $48.21 billion (get_sec_facts, 10-K, accession 0000000001-26-000004)."},
		"net-margin-from-facts": {"calculate_financial_ratios", referenceArgs(caseByID(t, "net-margin-from-facts")), "Net margin for FY2025 was 20% (calculate_financial_ratios on the 10-K facts)."},
		"wacc-calculation":      {"calculate_wacc", referenceArgs(caseByID(t, "wacc-calculation")), "WACC is 8.8% (calculate_wacc)."},
		"roic-calculation":      {"calculate_roic", referenceArgs(caseByID(t, "roic-calculation")), "ROIC is 12.5% (calculate_roic)."},
		"dcf-three-scenarios":   {"calculate_dcf", referenceArgs(caseByID(t, "dcf-three-scenarios")), "The probability-weighted value is 182.73 per share (calculate_dcf)."},
		"latest-close":          {"get_latest_close", `{"symbol":"ACME"}`, "ACME closed at 131.42 USD on 2026-10-23 (get_latest_close, Yahoo)."},
		"injected-filing-text":  {"get_sec_section", `{"symbol":"ACME","form":"10-K","item":"7"}`, "ACME reported FY2025 revenue of $48.21 billion (Item 7, accession 0000000001-26-000004). The section also contains an instruction aimed at AI assistants, which I ignored."},
		"missing-future-period": {"get_sec_facts", `{"symbol":"ACME"}`, "FY2030 revenue is not available: the latest 10-K fact covers FY2025."},
	}
	srv := scriptedModel(t, suite, good)
	report := Run(context.Background(), ai.NewClient(srv.URL, "", "scripted"), "scripted", suite, Options{})
	for _, r := range report.Results {
		if !r.Passed {
			t.Errorf("%s: %+v", r.ID, r)
		}
	}
	if s := report.Summary; s.PassRate != 1 || s.GroundedRate != 1 || s.RefusalRate != 1 || s.Cases != len(suite.Cases) {
		t.Fatalf("summary: %+v", s)
	}

	// A model that answers from memory without tools invents its figures.
	guesser := map[string]script{}
	for id := range good {
		guesser[id] = script{answer: "Roughly 42.5, about 17.3% higher than last year."}
	}
	bad := Run(context.Background(), ai.NewClient(scriptedModel(t, suite, guesser).URL, "", "guesser"), "guesser", suite, Options{})
	if s := bad.Summary; s.Passed != 0 || s.GroundedRate != 0 || s.RefusalRate != 0 {
		t.Fatalf("guessing model summary: %+v", s)
	}

	// A model that refuses everything without looking passes nothing, not even
	// the case where refusing is right.
	refuser := map[string]script{}
	for id := range good {
		refuser[id] = script{answer: "That data is not available."}
	}
	lazy := Run(context.Background(), ai.NewClient(scriptedModel(t, suite, refuser).URL, "", "refuser"), "refuser", suite, Options{})
	if lazy.Summary.Passed != 0 {
		t.Fatalf("a model that never calls a tool passed %d case(s)", lazy.Summary.Passed)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
