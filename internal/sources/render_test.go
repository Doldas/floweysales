package sources

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderCitationsLinksAndListsSources(t *testing.T) {
	list := []Source{
		{ID: "S1", URL: "https://example.com/a?id=1&token=abc#frag", Title: "Acme Q3 [results]", Access: AccessRead, PublishedAt: "2026-10-01", RetrievedAt: "2026-11-01T10:00:00Z", Tool: "read_source"},
		{ID: "S2", URL: "https://user:pw@example.com/b", Title: "Secret page", Access: AccessListed},
		{ID: "S3", URL: "https://sec.gov/x", DocumentID: "0000-24-1", Access: AccessRead, PublishedAt: "2026-02-01"},
	}
	out := RenderCitations("Revenue rose 5% [S1]. Costs fell [S2]. See [S9] and [S1].", list)
	for _, want := range []string{
		"[[S1]](https://example.com/a?id=1&token=REDACTED)",
		"Costs fell [S2].", // credentials in the address: no link
		"[S9]",
		"**Sources**",
		"- [S1] Acme Q3 (results); <https://example.com/a?id=1&token=REDACTED>; published 2026-10-01; retrieved 2026-11-01T10:00:00Z UTC; read by the agent",
		"- [S2] Secret page; publication date unknown; search result only (the page was not opened)",
		"- [S9] not a source retrieved in this run (unresolved citation)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "pw@") || strings.Contains(out, "#frag") || strings.Contains(out, "abc") {
		t.Errorf("credentials or fragment leaked:\n%s", out)
	}
	if strings.Contains(out, "[S3]") {
		t.Errorf("an uncited source was listed:\n%s", out)
	}
	if strings.Count(out, "- [S1]") != 1 {
		t.Errorf("a source cited twice must be listed once:\n%s", out)
	}
}

func TestRenderCitationsUnchangedWithoutMarkersAndIdempotentLinks(t *testing.T) {
	if got := RenderCitations("No markers here.", []Source{{ID: "S1"}}); got != "No markers here." {
		t.Fatalf("got %q", got)
	}
	linked := "Done [[S1]](https://example.com/a) and [S1](https://example.com/a)."
	out := RenderCitations(linked, []Source{{ID: "S1", URL: "https://example.com/a"}})
	if !strings.HasPrefix(out, linked) {
		t.Fatalf("existing links must not be wrapped again:\n%s", out)
	}
}

func TestExtractJSONCitationsKeepsStructure(t *testing.T) {
	in := `{"summary":"Revenue was $4.5 billion [S1].","items":[{"note":"Two filings [S2][S3]","n":12345678901234567890}],"plain":"x","k/ey":"y [S1]"}`
	out, cites, err := ExtractJSONCitations(in)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if strings.Contains(out, "[S") {
		t.Fatalf("markers left in the data: %s", out)
	}
	if got["summary"] != "Revenue was $4.5 billion." || got["plain"] != "x" {
		t.Fatalf("unexpected data: %v", got)
	}
	if !strings.Contains(out, "12345678901234567890") {
		t.Fatalf("number changed: %s", out)
	}
	want := map[string]string{"/summary": "S1", "/items/0/note": "S2,S3", "/k~1ey": "S1"}
	if len(cites) != len(want) {
		t.Fatalf("citations: %+v", cites)
	}
	for _, c := range cites {
		if strings.Join(c.Sources, ",") != want[c.Path] {
			t.Errorf("%s: %v", c.Path, c.Sources)
		}
	}
}

func TestExtractJSONCitationsNoMarkersReturnsInput(t *testing.T) {
	in := `{"b":1,"a":"x"}`
	out, cites, err := ExtractJSONCitations(in)
	if err != nil || out != in || cites != nil {
		t.Fatalf("%q %v %v", out, cites, err)
	}
	if _, _, err := ExtractJSONCitations("not json"); err == nil {
		t.Fatal("want an error")
	}
}

func TestJSONProseFeedsTheCitationCheck(t *testing.T) {
	r := NewRegistry()
	r.add(Source{Tool: "search_web", URL: "https://example.com/a", Title: "t", Class: ClassWebPage, Access: AccessRead, Excerpt: "Revenue was 4.5 billion dollars in 2025"})
	prose := JSONProse(`{"claims":[{"text":"Revenue was 9.9 billion [S1]"},{"text":"Revenue was 4.5 billion. [S1]"}],"cites":"[S7]"}`)
	found := Audit(prose, r)
	kinds := map[string]bool{}
	for _, f := range found {
		kinds[f.Kind] = true
	}
	if !kinds[FigureNotIn] || !kinds[Fabricated] {
		t.Fatalf("want figure_mismatch and fabricated, got %+v\nprose:\n%s", found, prose)
	}
	if JSONProse("plain text") != "plain text" {
		t.Fatal("non-JSON must pass through")
	}
}

func TestRegisterSECFilingsAndFacts(t *testing.T) {
	r := NewRegistry()
	filings := `{"retrieved_at":"2026-11-01T00:00:00Z","filings":[{"form":"10-K","accession":"0001-26-000001","filed":"2026-02-01","period_end":"2025-12-31","document_url":"https://www.sec.gov/Archives/a"},{"form":"10-Q","accession":"0001-26-000002","filed":"2026-05-01","document_url":"https://www.sec.gov/Archives/b"}]}`
	out, added := r.Register("get_sec_filings", "c1", `{"symbol":"ACME"}`, filings)
	if len(added) != 2 || added[0].ID != "S1" || added[0].Class != ClassPrimaryFiling || added[0].Access != AccessRead || added[0].DocumentID != "0001-26-000001" || added[0].PublishedAt != "2026-02-01" {
		t.Fatalf("filings: %+v", added)
	}
	if !strings.Contains(out, `"source_id":"S2"`) {
		t.Fatalf("model must see ids: %s", out)
	}
	facts := `{"retrieved_at":"2026-11-01T00:00:00Z","facts":[{"metric":"Revenue","value":4500000000,"unit":"USD","start":"2025-01-01","end":"2025-12-31","filed":"2026-02-01","form":"10-K","accession":"A1","fiscal_year":2025,"fiscal_period":"FY","filing_url":"https://www.sec.gov/Archives/a"},{"metric":"Net income","value":900000000,"unit":"USD","end":"2025-12-31","filed":"2026-02-01","form":"10-K","accession":"A1","filing_url":"https://www.sec.gov/Archives/a"}]}`
	out, added = r.Register("get_sec_facts", "c2", `{"symbol":"ACME"}`, facts)
	if len(added) != 1 || added[0].ID != "S3" || !strings.Contains(added[0].Excerpt, "Revenue: 4500000000 USD") || !strings.Contains(added[0].Excerpt, "Net income: 900000000 USD") {
		t.Fatalf("facts: %+v", added)
	}
	if strings.Count(out, `"source_id":"S3"`) != 2 {
		t.Fatalf("every fact needs its filing's id: %s", out)
	}
	if _, none := r.Register("get_sec_facts", "c3", `{}`, `{"error":"x"}`); none != nil {
		t.Fatal("an error result registers nothing")
	}
}

func TestSearchHitPublicationDateIsKept(t *testing.T) {
	r := NewRegistry()
	result := `{"retrieved_at_utc":"2026-11-01T00:00:00Z","provider":"searxng","results":[{"title":"A","url":"https://example.com/a","snippet":"s","published_at":"2026-10-30"},{"title":"B","url":"https://example.com/b","snippet":"s"}]}`
	_, added := r.Register("search_web", "c", `{"query":"q"}`, result)
	if len(added) != 2 || added[0].PublishedAt != "2026-10-30" || added[1].PublishedAt != "" {
		t.Fatalf("%+v", added)
	}
	if _, ok := parseDate("Oct 30, 2026"); !ok {
		t.Fatal("common search date format must parse")
	}
	// A dated hit can satisfy a recency limit; an undated one cannot.
	opts := Options{MaxAgeDays: 30}
	if p := opts.ageProblem(added[0]); p != "" && !strings.Contains(p, "days ago") {
		t.Fatalf("unexpected: %s", p)
	}
}
