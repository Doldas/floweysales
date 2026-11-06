package sources

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

type goldenWant struct {
	Verdict   string `json:"verdict"`
	Code      string `json:"code"`
	Conflicts int    `json:"conflicts"`
	Injection bool   `json:"injection"`
	Finding   string `json:"finding"`
}

type golden struct {
	Name     string       `json:"name"`
	Category string       `json:"category"`
	Sources  []Source     `json:"sources"`
	Answer   string       `json:"answer"`
	Strict   bool         `json:"strict"`
	Want     []goldenWant `json:"want"`
}

func loadGolden(t *testing.T) []golden {
	t.Helper()
	raw, err := os.ReadFile("testdata/claims_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var list []golden
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func goldenNamed(t *testing.T, name string) golden {
	t.Helper()
	for _, g := range loadGolden(t) {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("no golden fixture %q", name)
	return golden{}
}

// registryFor builds a registry with the ids the fixture gives its sources.
func registryFor(list []Source) *Registry {
	r := NewRegistry()
	for _, s := range list {
		r.byID[s.ID] = len(r.list)
		r.list = append(r.list, s)
	}
	return r
}

var fixedNow = time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)

func TestClaimGoldenFixtures(t *testing.T) {
	cats := map[string]int{}
	for _, g := range loadGolden(t) {
		cats[g.Category]++
		t.Run(g.Name, func(t *testing.T) {
			rep := VerifyClaims(g.Answer, registryFor(g.Sources), ClaimOptions{Strict: g.Strict, Now: fixedNow})
			if len(rep.Claims) != len(g.Want) {
				t.Fatalf("got %d claims, want %d: %+v", len(rep.Claims), len(g.Want), rep.Claims)
			}
			findings := ClaimFindings(rep, nil, g.Strict)
			for i, w := range g.Want {
				c := rep.Claims[i]
				if c.Verdict != w.Verdict {
					t.Fatalf("verdict %q (%s: %s), want %q", c.Verdict, c.Code, c.Explanation, w.Verdict)
				}
				if w.Code != "" && c.Code != w.Code {
					t.Fatalf("code %q (%s), want %q", c.Code, c.Explanation, w.Code)
				}
				if w.Conflicts != len(c.Conflicts) {
					t.Fatalf("conflicts = %d, want %d", len(c.Conflicts), w.Conflicts)
				}
				if c.InjectionSuspected != w.Injection {
					t.Fatalf("injection_suspected = %v, want %v", c.InjectionSuspected, w.Injection)
				}
				if c.VerifierVersion != VerifierVersion || !strings.HasSuffix(c.VerifiedAt, "Z") || c.Limitations == "" || c.Explanation == "" {
					t.Fatalf("record lacks provenance: %+v", c)
				}
				if c.Verdict == Supported && len(c.Figures) > 0 && len(c.Spans) == 0 {
					t.Fatalf("a supported figure claim must carry its source span: %+v", c)
				}
				if w.Finding != "" {
					found := false
					for _, f := range findings {
						found = found || f.Kind == w.Finding
					}
					if !found {
						t.Fatalf("want a %s finding, got %+v", w.Finding, findings)
					}
				}
				// Uncertainty is never proof, and a misread number never passes quietly.
				flagged := c.Verdict == Contradicted || (c.Verdict == Insufficient && (g.Strict || flaggedAlways[c.Code]))
				if flagged && len(findings) == 0 {
					t.Fatalf("a %s claim (%s) must raise a finding", c.Verdict, c.Code)
				}
				if c.Verdict == Supported && len(findings) > 0 {
					t.Fatalf("a supported claim raised findings: %+v", findings)
				}
			}
		})
	}
	for _, cat := range []string{"supported", "wrong_metric", "wrong_period", "wrong_entity", "wrong_unit", "wrong_direction", "misleading_context", "cherry_picked", "contradicted", "unsupported", "not_checked", "prompt_injection", "abstention"} {
		if cats[cat] == 0 {
			t.Errorf("no golden fixture in category %s", cat)
		}
	}
}

func TestClaimSpanQuotesTheSource(t *testing.T) {
	g := goldenNamed(t, "supported_table_revenue")
	r := registryFor(g.Sources)
	c := VerifyClaims(g.Answer, r, ClaimOptions{}).Claims[0]
	sp := c.Spans[0]
	src, _ := r.Get(sp.Source)
	if src.Excerpt[sp.Start:sp.End] != sp.Match || sp.Match != "4,500" || !strings.Contains(sp.Before, "Total revenue") {
		t.Fatalf("span does not point at the source text: %+v", sp)
	}
	if sp.URL == "" || sp.RetrievedAt == "" || sp.Title == "" {
		t.Fatalf("span lost the source metadata: %+v", sp)
	}
	if c.Entity != "Acme" || c.Metric != "revenue" || c.Period != "FY2024" || c.Unit != "USD" || !strings.Contains(c.Assertion, "= 4.5 billion") {
		t.Fatalf("record = %+v", c)
	}
}

func TestClaimCalculationIsShown(t *testing.T) {
	g := goldenNamed(t, "arithmetic_error")
	c := VerifyClaims(g.Answer, registryFor(g.Sources), ClaimOptions{}).Claims[0]
	if c.Calculation != "(4500000000 - 3900000000) / 3900000000 = 15.4%" {
		t.Fatalf("calculation = %q", c.Calculation)
	}
}

func TestClaimVerificationIsBoundedAndModelFree(t *testing.T) {
	g := goldenNamed(t, "supported_table_revenue")
	answer := strings.Repeat("Acme's total revenue was $4.5 billion in fiscal 2024 [S1]. ", 80)
	start := time.Now()
	rep := VerifyClaims(answer, registryFor(g.Sources), ClaimOptions{})
	if len(rep.Claims) != maxClaims || rep.Stats.Skipped != 80-maxClaims {
		t.Fatalf("claims = %d, skipped = %d", len(rep.Claims), rep.Stats.Skipped)
	}
	if rep.Stats.ModelCalls != 0 || rep.Stats.Version != VerifierVersion || time.Since(start) > 2*time.Second {
		t.Fatalf("stats = %+v after %v", rep.Stats, time.Since(start))
	}
}

func TestClaimFindingsDoNotRepeatAnotherCheck(t *testing.T) {
	g := goldenNamed(t, "wrong_metric_net_income_for_revenue")
	rep := VerifyClaims(g.Answer, registryFor(g.Sources), ClaimOptions{})
	first := ClaimFindings(rep, nil, true)
	if len(first) != 1 || first[0].Kind != ClaimContradicted {
		t.Fatalf("findings = %+v", first)
	}
	if again := ClaimFindings(rep, first, true); len(again) != 0 {
		t.Fatalf("sentence flagged twice: %+v", again)
	}
	body, removed := Block(g.Answer+" Other text.", first)
	if len(removed) != 1 || strings.Contains(body, "net income") {
		t.Fatalf("block = %q, %+v", body, removed)
	}
}
