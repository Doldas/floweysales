package websearch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"floweysales/internal/sourcetrust"
)

var trustNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func renderTrust(t *testing.T, provider, query string, results []Result, opts RenderOptions) (rendered, string) {
	t.Helper()
	opts.Now = trustNow
	resp := Response{Provider: provider, Query: query, RetrievedAt: trustNow, Results: results}
	raw, err := Render(Outcome{Response: &resp}, func(u string) (string, bool) { return u, true }, opts)
	if err != nil {
		t.Fatal(err)
	}
	var out rendered
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out, raw
}

func TestRenderOfficialQuarterBeatsNewerRedditOnFinancialClaims(t *testing.T) {
	out, _ := renderTrust(t, Brave, "Acme Q3 2024 revenue", []Result{
		{Title: "Acme revenue is nuts", URL: "https://www.reddit.com/r/stocks/comments/1/acme/", Snippet: "revenue 9 billion lol", PublishedAt: "2026-11-06T08:00:00Z", Type: TypeWebResult},
		{Title: "Acme Q3 2024 10-Q", URL: "https://www.sec.gov/Archives/acme-10q.htm", Snippet: "Revenue for the quarter ended September 30, 2024", PublishedAt: "2024-10-30", Type: TypeWebResult},
	}, RenderOptions{})
	if out.Results[0].URL != "https://www.sec.gov/Archives/acme-10q.htm" {
		t.Fatalf("order: %s first", out.Results[0].URL)
	}
	top, second := out.Results[0].Trust, out.Results[1].Trust
	if top.Type != sourcetrust.TypeOfficialFiling || top.Role != sourcetrust.RolePrimary || top.Period != "Q3 2024" || top.EvidenceState != sourcetrust.StateSnippetOnly {
		t.Fatalf("%+v", top)
	}
	if second.Type != sourcetrust.TypeCommunity || second.Role != sourcetrust.RoleSentiment {
		t.Fatalf("%+v", second)
	}
	if len(top.WhyRanked) == 0 || top.Rank != 1 || second.Rank != 2 {
		t.Fatalf("ranking must be explained: %+v %+v", top.WhyRanked, second.Rank)
	}
	if out.Ranking == nil || out.Ranking.Profile != "facts_first" || !strings.Contains(out.Ranking.Policy, "Q3 2024") {
		t.Fatalf("%+v", out.Ranking)
	}
}

func TestRenderRedditThreadForSentimentQuestion(t *testing.T) {
	out, _ := renderTrust(t, SearXNG, "what do people on reddit think about Acme earnings", []Result{
		{Title: "Acme 10-Q", URL: "https://www.sec.gov/Archives/acme-10q.htm", Snippet: "quarterly report", PublishedAt: "2026-10-30", Type: TypeWebResult},
		{Title: "Acme earnings reaction thread", URL: "https://www.reddit.com/r/stocks/comments/2/acme_reaction/", Snippet: "what a print", PublishedAt: "2026-11-06T09:30:00Z", Type: TypeWebResult},
	}, RenderOptions{})
	first := out.Results[0]
	if first.Trust.Type != sourcetrust.TypeCommunity || first.Trust.Role != sourcetrust.RoleSentiment {
		t.Fatalf("a recent thread is wanted for a sentiment question: %+v", first.Trust)
	}
	if first.PublishedAt != "2026-11-06T09:30:00Z" || first.Trust.DateSource != sourcetrust.DateSearchProvider {
		t.Fatalf("thread timestamp must be kept and attributed: %+v", first)
	}
	if first.Trust.EvidenceState != sourcetrust.StateSnippetOnly {
		t.Fatal("a listed thread is a snippet")
	}
}

func TestRenderSyndicatedDuplicatesAndFilteredList(t *testing.T) {
	out, _ := renderTrust(t, Tavily, "Acme profit jumps", []Result{
		{Title: "Acme profit jumps on cloud demand | Example Finance", URL: "https://finance.example.com/acme", PublishedAt: "2026-11-03", Type: TypeWebResult},
		{Title: "Acme profit jumps on cloud demand - Reuters", URL: "https://www.reuters.com/acme", PublishedAt: "2026-11-02", Type: TypeWebResult},
	}, RenderOptions{})
	if len(out.Results) != 1 || out.Results[0].URL != "https://www.reuters.com/acme" {
		t.Fatalf("%+v", out.Results)
	}
	if got := out.Results[0].Trust.AlsoSeenAt; len(got) != 1 || got[0] != "https://finance.example.com/acme" {
		t.Fatalf("%v", got)
	}
	if len(out.Ranking.Filtered) != 1 {
		t.Fatalf("%+v", out.Ranking.Filtered)
	}
}

func TestRenderBadMetadataNeverBecomesFreshness(t *testing.T) {
	out, _ := renderTrust(t, Brave, "Acme news", []Result{
		{Title: "Future post", URL: "https://blog.example/a", PublishedAt: "2031-01-01", Type: TypeWebResult},
		{Title: "Undated post", URL: "https://blog.example/b", Type: TypeWebResult},
		{Title: "Relative age only", URL: "https://blog.example/c", Age: "2 days ago", Type: TypeWebResult},
	}, RenderOptions{})
	for _, r := range out.Results {
		if r.PublishedAt != "" || r.Trust.DateConfidence != sourcetrust.ConfNone || r.Trust.DateSource != sourcetrust.DateUnknown {
			t.Fatalf("invented a date: %+v", r)
		}
	}
	joined := strings.Join(out.Results[0].Trust.DateNotes, " ") + strings.Join(out.Results[1].Trust.DateNotes, " ") + strings.Join(out.Results[2].Trust.DateNotes, " ")
	if !strings.Contains(joined, "future") {
		t.Fatalf("the rejected date must be explained: %s", joined)
	}
}

func TestRenderSameShapeForEveryProvider(t *testing.T) {
	for _, p := range []string{SearXNG, Brave, Tavily, GoogleGrounding, GoogleCSE} {
		typ := TypeWebResult
		if p == GoogleGrounding {
			typ = TypeGroundedSource
		}
		out, _ := renderTrust(t, p, "Acme annual report FY2023", []Result{
			{Title: "Acme annual report", URL: "https://investors.acme.com/fy2023", Snippet: "FY2023 annual report", Type: typ},
		}, RenderOptions{})
		tr := out.Results[0].Trust
		if tr == nil || tr.Type != sourcetrust.TypeCompanyIR || tr.Role != sourcetrust.RoleSelfReported || tr.Period != "FY2023" || tr.EvidenceState != sourcetrust.StateSnippetOnly {
			t.Fatalf("%s: %+v", p, tr)
		}
		if out.Results[0].Provider != p {
			t.Fatalf("provider lost for %s", p)
		}
	}
}

func TestRenderProfileAndWindowAreApplied(t *testing.T) {
	results := []Result{
		{Title: "Acme old headline", URL: "https://www.reuters.com/old", PublishedAt: "2026-01-01", Type: TypeWebResult},
		{Title: "Acme headline", URL: "https://www.reuters.com/new", PublishedAt: "2026-11-05", Type: TypeWebResult},
	}
	out, _ := renderTrust(t, Brave, "Acme latest news", results, RenderOptions{WindowDays: 14, Profile: "balanced"})
	if out.Results[0].URL != "https://www.reuters.com/new" || out.Ranking.Profile != "balanced" || out.Ranking.WindowDays != 14 {
		t.Fatalf("%+v", out.Ranking)
	}
	if len(out.Results) != 2 {
		t.Fatal("results outside the window must remain as a fallback")
	}
	if !strings.Contains(strings.Join(out.Results[1].Trust.WhyRanked, " "), "outside the 14-day window") {
		t.Fatalf("%v", out.Results[1].Trust.WhyRanked)
	}
	// An unknown profile falls back to the default instead of failing.
	out, _ = renderTrust(t, Brave, "Acme news", results, RenderOptions{Profile: "bogus"})
	if out.Ranking.Profile != "facts_first" {
		t.Fatal(out.Ranking.Profile)
	}
}

func TestRenderGroundedSupportsFollowRanking(t *testing.T) {
	resp := Response{Provider: GoogleGrounding, Query: "Acme Q3 2024 revenue", RetrievedAt: trustNow, Answer: "Revenue rose.",
		Results: []Result{
			{Title: "Reddit", URL: "https://www.reddit.com/r/a/comments/1/x/", Type: TypeGroundedSource},
			{Title: "Acme 10-Q Q3 2024", URL: "https://www.sec.gov/acme", Type: TypeGroundedSource},
		},
		Supports: []Support{{Text: "Revenue rose.", Sources: []int{1}}}}
	raw, err := Render(Outcome{Response: &resp}, func(u string) (string, bool) { return u, true }, RenderOptions{Now: trustNow})
	if err != nil {
		t.Fatal(err)
	}
	var out rendered
	_ = json.Unmarshal([]byte(raw), &out)
	if out.Results[0].URL != "https://www.sec.gov/acme" {
		t.Fatalf("%+v", out.Results)
	}
	if got := out.Grounded.Supports[0].Sources; len(got) != 1 || got[0] != 0 {
		t.Fatalf("a support must point at the new position of its source: %v", got)
	}
	if out.Results[0].Trust.EvidenceState != sourcetrust.StateSnippetOnly {
		t.Fatal("a grounded source is not a page that was read")
	}
}

func TestRenderStaysBounded(t *testing.T) {
	var results []Result
	long := strings.Repeat("Acme quarterly results and outlook. ", 40)
	for i := 0; i < 8; i++ {
		results = append(results, Result{Title: "Acme result " + string(rune('A'+i)) + " " + long[:60], URL: "https://site" + string(rune('a'+i)) + ".example/p", Snippet: long, PublishedAt: "2026-10-01", Type: TypeWebResult})
	}
	_, raw := renderTrust(t, Brave, "Acme Q3 2026 revenue", results, RenderOptions{})
	if len(raw) > MaxOutput {
		t.Fatalf("output %d bytes", len(raw))
	}
}
