package sources

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSourceTypeAllowAndDenyListsDecideWhatCounts(t *testing.T) {
	r := registry(t)
	const web = "Acme is great [S2]."
	const filing = "Net income was 910.5 million [S3]."

	// Primary-only without a list accepts filings only.
	if got := AuditWith(web, r, Options{PrimaryOnly: true}); kinds(got) != "not_primary" {
		t.Fatalf("default primary mode: %s", kinds(got))
	}
	// Allowing web pages as well turns the default off.
	if got := AuditWith(web, r, Options{PrimaryOnly: true, AllowClasses: []string{ClassPrimaryFiling, ClassWebPage}}); len(got) != 0 {
		t.Fatalf("an allowed type must pass: %+v", got)
	}
	// An allow list names the only types that count.
	if got := AuditWith(filing, r, Options{AllowClasses: []string{ClassWebPage}}); kinds(got) != "excluded_source" {
		t.Fatalf("type outside the allow list: %s", kinds(got))
	}
	// Deny wins over allow.
	got := AuditWith(filing, r, Options{PrimaryOnly: true, AllowClasses: []string{ClassPrimaryFiling}, DenyClasses: []string{ClassPrimaryFiling}})
	if kinds(got) != "excluded_source" || !strings.Contains(got[0].Detail, "excluded") {
		t.Fatalf("deny list: %+v", got)
	}
	// Without PrimaryOnly or lists, nothing is excluded.
	if got := AuditWith(web+" "+filing, r, Options{}); len(got) != 0 {
		t.Fatalf("no settings, no findings: %+v", got)
	}
	if !ValidClass(ClassWebPage) || ValidClass("rumor") {
		t.Fatal("ValidClass")
	}
}

func TestRecencyLimitFlagsOldAndUndatedSources(t *testing.T) {
	r := registry(t)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	// S3 was filed on 2026-02-01, 275 days earlier.
	if got := AuditWith("Net income was 910.5 million [S3].", r, Options{MaxAgeDays: 365, Now: now}); len(got) != 0 {
		t.Fatalf("inside the limit: %+v", got)
	}
	got := AuditWith("Net income was 910.5 million [S3].", r, Options{MaxAgeDays: 90, Now: now})
	if kinds(got) != "stale" || !strings.Contains(got[0].Detail, "2026-02-01") || !strings.Contains(got[0].Detail, "275 days") {
		t.Fatalf("old source: %+v", got)
	}
	// A search hit has no publication date: it cannot be shown to be recent.
	got = AuditWith("Acme is great [S2].", r, Options{MaxAgeDays: 90, Now: now})
	if kinds(got) != "stale" || !strings.Contains(got[0].Detail, "no publication date") {
		t.Fatalf("undated source: %+v", got)
	}
	// No limit, no check.
	if got := AuditWith("Acme is great [S2].", r, Options{Now: now}); len(got) != 0 {
		t.Fatalf("limit off: %+v", got)
	}
}

func TestBlockRemovesFlaggedSentencesAndSaysSo(t *testing.T) {
	r := registry(t)
	answer := "Net income was 910.5 million [S3]. Revenue was 4,200 million [S1]. Acme looks solid.\nCosts rose 12%."
	found := Audit(answer, r)
	if kinds(found) != "snippet_only" {
		t.Fatalf("setup: %s", kinds(found))
	}
	got, removed := Block(answer, found)
	if strings.Contains(got, "4,200") || !strings.Contains(got, "910.5") || !strings.Contains(got, "Acme looks solid") {
		t.Fatalf("blocked answer: %q", got)
	}
	if !strings.Contains(got, "Removed 1 statement(s)") || !strings.Contains(got, "snippet_only") || len(removed) != 1 {
		t.Fatalf("the removal must be disclosed: %q", got)
	}
	if again := Audit(got, r); len(again) != 0 {
		t.Fatalf("what is left must pass the check: %+v", again)
	}
}

func TestBlockAbstainsWhenNothingIsLeft(t *testing.T) {
	r := registry(t)
	answer := "Revenue was 4,200 million [S1]."
	got, removed := Block(answer, Audit(answer, r))
	if !strings.HasPrefix(got, "Insufficient verifiable evidence") || strings.Contains(got, "4,200") || len(removed) != 1 {
		t.Fatalf("abstention: %q", got)
	}
	// Findings that match no sentence change nothing.
	if got, removed := Block("Fine.", []Finding{{Kind: "uncited", Sentence: "Other."}}); got != "Fine." || removed != nil {
		t.Fatalf("unmatched: %q %v", got, removed)
	}
}

func TestBlockRemovesOverlongSentencesByTheirShownForm(t *testing.T) {
	r := registry(t)
	long := "Revenue was 4,200 million [S1] " + strings.Repeat("and more words ", 30) + "."
	got, _ := Block(long+" Kept sentence.", Audit(long+" Kept sentence.", r))
	if strings.Contains(got, "4,200") || !strings.Contains(got, "Kept sentence") {
		t.Fatalf("%q", got)
	}
}

func TestLinkCheckFlagsOnlyCitedUnreadSourcesAndChecksEachOnce(t *testing.T) {
	r := registry(t)
	calls := map[string]int{}
	check := func(_ context.Context, u string) error {
		calls[u]++
		if strings.Contains(u, "blog.example") {
			return errors.New("HTTP status 404")
		}
		return nil
	}
	var cache LinkCache
	answer := "Acme is great [S2]. Revenue was 4,200 million [S1]. Net income was 910.5 million [S3]. Acme is great again [S2]."
	got := cache.Findings(context.Background(), answer, r, check)
	if len(got) != 2 || got[0].Kind != Unreachable || got[0].Source != "S2" || !strings.Contains(got[0].Detail, "404") {
		t.Fatalf("findings: %+v", got)
	}
	if calls["https://blog.example/acme"] != 1 || calls["https://news.example/acme-q3"] != 1 || len(calls) != 2 {
		t.Fatalf("each address is checked once and a source the agent read is not: %v", calls)
	}
	// A correction round asks again: the answers are remembered.
	cache.Findings(context.Background(), answer, r, check)
	if calls["https://blog.example/acme"] != 1 {
		t.Fatalf("not cached: %v", calls)
	}
	if cache.Findings(context.Background(), answer, r, nil) != nil {
		t.Fatal("no checker, no findings")
	}
}

func TestLinkCheckIsBoundedAndIgnoresCancellation(t *testing.T) {
	r := NewRegistry()
	var hits []map[string]any
	for i := 0; i < MaxLinkChecks+3; i++ {
		hits = append(hits, map[string]any{"title": "t", "url": "https://h" + string(rune('a'+i)) + ".example/p", "snippet": "s"})
	}
	r.Register("search_web", "c", `{}`, mustJSON(t, map[string]any{"results": hits}))
	var cited []string
	for i := range hits {
		cited = append(cited, "See [S"+string(rune('1'+i%9))+"].")
	}
	n := 0
	var cache LinkCache
	cache.Findings(context.Background(), strings.Join(cited[:9], " "), r, func(context.Context, string) error { n++; return errors.New("down") })
	if n != MaxLinkChecks {
		t.Fatalf("checks = %d, want the bound %d", n, MaxLinkChecks)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var fresh LinkCache
	if got := fresh.Findings(ctx, "See [S1].", r, func(context.Context, string) error { return errors.New("canceled") }); len(got) != 0 {
		t.Fatalf("a cancelled run says nothing about the link: %+v", got)
	}
}

func TestRegistryFromKeepsIDsAndContinuesNumbering(t *testing.T) {
	old := registry(t)
	r := RegistryFrom(old.All())
	if s, ok := r.Get("S3"); !ok || s.Class != ClassPrimaryFiling {
		t.Fatalf("S3: %+v", s)
	}
	_, added := r.Register("search_web", "c9", `{}`, `{"results":[{"title":"n","url":"https://new.example","snippet":"x"}]}`)
	if len(added) != 1 || added[0].ID != "S4" {
		t.Fatalf("numbering: %+v", added)
	}
	broken := []Source{{ID: "S1"}, {ID: "S3"}, {ID: "S2"}}
	if got := RegistryFrom(broken).All(); len(got) != 1 {
		t.Fatalf("a gap ends the restore: %+v", got)
	}
}

func TestCompactBoundsSourcesAndExcerpts(t *testing.T) {
	in := []Source{{ID: "S1", Excerpt: strings.Repeat("é", 50)}, {ID: "S2"}, {ID: "S3"}}
	got := Compact(in, 2, 11)
	if len(got) != 2 || len(got[0].Excerpt) > 11 || got[0].ID != "S1" {
		t.Fatalf("%+v", got)
	}
	if len(in[0].Excerpt) != 100 {
		t.Fatal("the input must not change")
	}
}
