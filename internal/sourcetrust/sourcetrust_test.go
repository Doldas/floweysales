package sourcetrust

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func TestClassify(t *testing.T) {
	cases := []struct {
		url, title, snippet string
		typ, role           string
	}{
		{"https://www.sec.gov/Archives/edgar/data/1/10q.htm", "", "", TypeOfficialFiling, RolePrimary},
		{"https://www.reddit.com/r/stocks/comments/abc/acme_q3/", "", "", TypeCommunity, RoleSentiment},
		{"https://old.reddit.com/r/investing/", "", "", TypeCommunity, RoleSentiment},
		{"https://www.reuters.com/markets/acme", "", "", TypeEstablishedNews, RoleSecondary},
		{"https://investors.acme.com/news/q3", "", "", TypeCompanyIR, RoleSelfReported},
		{"https://www.acme.com/investor-relations/results.pdf", "", "", TypeCompanyIR, RoleSelfReported},
		{"https://www.prnewswire.com/news/acme", "", "", TypeCompanyIR, RoleSelfReported},
		{"https://seekingalpha.com/article/1", "", "", TypeProfessionalAnalysis, RoleOpinion},
		{"https://www.bls.gov/cpi/", "", "", TypeGovernmentData, RolePrimary},
		{"https://data.statistics.gov.uk/x", "", "", TypeGovernmentData, RolePrimary},
		{"https://www.nature.com/articles/x", "", "", TypePeerReviewed, RolePrimary},
		{"https://finance.yahoo.com/quote/ACME", "", "", TypeUnknown, RoleUnknown},
		{"https://random-blog.example/post", "", "", TypeUnknown, RoleUnknown},
		{"https://random-blog.example/post", "Buy now", "Sponsored content: ten stocks", TypeMarketing, RoleSelfReported},
		{"https://forum.example.org/t/1", "", "", TypeCommunity, RoleSentiment},
		{"not a url %%", "", "", TypeUnknown, RoleUnknown},
	}
	for _, c := range cases {
		got := Classify(c.url, c.title, c.snippet)
		if got.Type != c.typ || got.Role != c.role {
			t.Errorf("Classify(%q) = %s/%s, want %s/%s", c.url, got.Type, got.Role, c.typ, c.role)
		}
		if got.Basis == "" {
			t.Errorf("Classify(%q) has no basis", c.url)
		}
	}
	// A look-alike host is not the real one.
	if got := Classify("https://sec.gov.evil.example/x", "", ""); got.Type == TypeOfficialFiling {
		t.Fatalf("look-alike host classified as an official filing: %+v", got)
	}
}

func TestFinancialFactGuard(t *testing.T) {
	for _, role := range []string{RoleSentiment, RoleOpinion, RoleUnknown} {
		if CanSupportFinancialFact(role) {
			t.Errorf("role %s must not support a financial fact", role)
		}
	}
	for _, role := range []string{RolePrimary, RoleSelfReported, RoleSecondary} {
		if !CanSupportFinancialFact(role) {
			t.Errorf("role %s should be able to support a fact", role)
		}
	}
}

func TestParseDateKeepsPrecision(t *testing.T) {
	for _, raw := range []string{"2024-10-30", "2024-10-30T09:15:00+02:00", "October 30, 2024", "Oct. 30, 2024", "30 October 2024", "D:20241030091500+02'00'", "Wed, 30 Oct 2024 09:15:00 GMT"} {
		if _, ok := ParseDate(raw); !ok {
			t.Errorf("ParseDate(%q) failed", raw)
		}
	}
	if _, ok := ParseDate("yesterday"); ok {
		t.Fatal("relative words are not dates")
	}
	if got := ParsePDFDate("D:20241030091500+02'00'"); got != "2024-10-30T09:15:00+02:00" {
		t.Fatalf("pdf date = %q", got)
	}
	if got := ParsePDFDate("D:2024"); got != "2024-01-01T00:00:00Z" {
		t.Fatalf("pdf year-only date = %q", got)
	}
	if ParsePDFDate("hello") != "" {
		t.Fatal("not a pdf date")
	}
}

func TestValidateDateRejectsFutureAndAncient(t *testing.T) {
	if _, why := ValidateDate("2031-01-01", now); why == "" {
		t.Fatal("a date in the future must be rejected")
	}
	if _, why := ValidateDate("1969-01-01", now); why == "" {
		t.Fatal("an implausibly old date must be rejected")
	}
	if _, why := ValidateDate("2026-11-08", now); why != "" {
		t.Fatalf("one day ahead is a time zone, not an error: %s", why)
	}
}

func TestFutureDatedMetadataIsNotFreshness(t *testing.T) {
	m := ForSearchResult("https://blog.example/x", "t", "s", "2030-05-05", "2026-11-07T12:00:00Z", now)
	if m.PublishedAt != "" || m.DateConfidence != ConfNone || m.DateSource != DateUnknown {
		t.Fatalf("future date must be dropped, got %+v", m)
	}
	if len(m.DateNotes) == 0 || !strings.Contains(m.DateNotes[0], "future") {
		t.Fatalf("rejection must be explained: %v", m.DateNotes)
	}
}

func TestUnknownDateStaysUnknown(t *testing.T) {
	m := ForSearchResult("https://blog.example/x", "t", "s", "", "2026-11-07T12:00:00Z", now)
	if m.PublishedAt != "" || m.DateConfidence != ConfNone {
		t.Fatalf("unknown date was invented: %+v", m)
	}
	if m.RetrievedAt == "" || m.PublishedAt == m.RetrievedAt {
		t.Fatal("retrieval time must not become the publication date")
	}
}

func TestExtractPageDatesJSONLD(t *testing.T) {
	page := `<html><head><script type="application/ld+json">{"@context":"https://schema.org","@graph":[{"@type":"NewsArticle","datePublished":"2024-10-30T08:00:00Z","dateModified":"2026-11-06T10:00:00Z"}]}</script></head></html>`
	pd := ExtractPageDates(page)
	if pd.Published != "2024-10-30T08:00:00Z" || pd.Modified != "2026-11-06T10:00:00Z" || pd.Source != DateJSONLD || pd.Conf != ConfHigh {
		t.Fatalf("%+v", pd)
	}
}

func TestExtractPageDatesMetaAndTime(t *testing.T) {
	meta := `<meta property="article:published_time" content="2024-10-30T08:00:00+00:00"><meta property="article:modified_time" content="2025-01-02">`
	pd := ExtractPageDates(meta)
	if pd.Published == "" || pd.Modified != "2025-01-02" || pd.Source != DateMeta {
		t.Fatalf("%+v", pd)
	}
	pd = ExtractPageDates(`<p>Posted <time datetime="2024-09-01">Sep 1</time></p>`)
	if pd.Published != "2024-09-01" || pd.Conf != ConfLow || pd.Source != DateTimeElement {
		t.Fatalf("%+v", pd)
	}
	if pd := ExtractPageDates(`<p>nothing</p>`); pd.Published != "" || pd.Modified != "" {
		t.Fatalf("%+v", pd)
	}
	if pd := ExtractPageDates(`<script type="application/ld+json">{broken</script>`); pd.Published != "" {
		t.Fatal("malformed JSON-LD must be ignored")
	}
}

func TestChangedButStaleArticle(t *testing.T) {
	// A page updated yesterday whose statement was published two years ago.
	m := ForSearchResult("https://news.example/acme", "Acme results", "s", "", "2026-11-07T12:00:00Z", now)
	m.ApplyPage(`<meta property="article:modified_time" content="2026-11-06T10:00:00Z">`, "Acme revenue was 5 billion", false, "", "", "", now)
	if m.ModifiedAt == "" || m.PublishedAt != "" {
		t.Fatalf("modified date must not become a publication date: %+v", m)
	}
	joined := strings.Join(m.DateNotes, " ")
	if !strings.Contains(joined, "only an update date is known") {
		t.Fatalf("missing explanation: %v", m.DateNotes)
	}
	// Freshness must not be credited from the update stamp.
	_, why := Score(&Candidate{Meta: m}, 1, Options{Intent: Intent{Kind: IntentGeneral, Latest: true}, Now: now})
	if !strings.Contains(strings.Join(why, " "), "update date is not used as freshness") {
		t.Fatalf("ranking must say the update date is not freshness: %v", why)
	}
}

func TestUnchangedDataWithNewUpdateStamp(t *testing.T) {
	old := &Meta{Type: TypeEstablishedNews, Role: RoleSecondary, PublishedAt: "2023-02-01", ModifiedAt: "2026-11-06T10:00:00Z", EvidenceState: StatePageRead}
	fresh := &Meta{Type: TypeEstablishedNews, Role: RoleSecondary, PublishedAt: "2026-11-01", EvidenceState: StatePageRead}
	in := Options{Intent: Intent{Kind: IntentGeneral, Latest: true}, Now: now}
	so, _ := Score(&Candidate{Index: 0, Meta: old}, 2, in)
	sf, _ := Score(&Candidate{Index: 1, Meta: fresh}, 2, in)
	if sf <= so {
		t.Fatalf("an article touched yesterday but published in 2023 (%.1f) must not beat a fresh one (%.1f)", so, sf)
	}
}

func TestConflictingDatesAreExposed(t *testing.T) {
	m := ForSearchResult("https://news.example/a", "t", "s", "2024-03-01", "2026-11-07T12:00:00Z", now)
	m.ApplyPage(`<meta property="article:published_time" content="2025-06-15T00:00:00Z">`, "", false, "", "", "", now)
	if m.DateConfidence != ConfLow {
		t.Fatalf("conflicting dates must lower confidence: %+v", m)
	}
	if len(m.DateNotes) == 0 || !strings.Contains(m.DateNotes[0], "dates conflict") {
		t.Fatalf("conflict must be listed: %v", m.DateNotes)
	}
	if m.PublishedAt != "2025-06-15T00:00:00Z" {
		t.Fatalf("the page's own structured date is the better source: %s", m.PublishedAt)
	}
}

func TestModifiedBeforePublishedIgnored(t *testing.T) {
	m := &Meta{}
	m.SetPublished("2025-05-01", DateMeta, ConfHigh, now)
	m.SetModified("2024-01-01", DateMeta, now)
	if m.ModifiedAt != "" || len(m.DateNotes) == 0 {
		t.Fatalf("%+v", m)
	}
}

func TestPDFFileDatesStaySeparate(t *testing.T) {
	m := ForSearchResult("https://investors.acme.com/q3.pdf", "Acme Interim report", "", "", "2026-11-07T12:00:00Z", now)
	text := "Acme AB Interim report January-September 2024. Published: October 30, 2024. Net sales..."
	m.ApplyPage("", text, true, "2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z", "Tue, 03 Nov 2026 10:00:00 GMT", now)
	if m.PublishedAt != "October 30, 2024" || m.DateSource != DatePDFCover {
		t.Fatalf("report date must come from the cover: %+v", m)
	}
	if m.FileCreatedAt != "2026-10-01T00:00:00Z" || m.FileModifiedAt != "2026-10-02T00:00:00Z" || m.HTTPLastModified == "" {
		t.Fatalf("file dates lost: %+v", m)
	}
	if m.PublishedAt == m.FileCreatedAt || m.PublishedAt == m.RetrievedAt {
		t.Fatal("file or retrieval date used as report date")
	}
	if m.EvidenceState != StatePDFText {
		t.Fatalf("state = %s", m.EvidenceState)
	}
}

func TestPDFWithOnlyFileDatesHasNoReportDate(t *testing.T) {
	m := ForSearchResult("https://investors.acme.com/q3.pdf", "Acme", "", "", "2026-11-07T12:00:00Z", now)
	m.ApplyPage("", "Acme annual report with no stated date", true, "2026-10-01T00:00:00Z", "", "", now)
	if m.PublishedAt != "" || m.DateConfidence != ConfNone {
		t.Fatalf("file date became a report date: %+v", m)
	}
	if !strings.Contains(strings.Join(m.DateNotes, " "), "only file dates are known") {
		t.Fatalf("%v", m.DateNotes)
	}
}

func TestExtractPeriod(t *testing.T) {
	cases := map[string]string{
		"Acme Q3 2024 results":                     "Q3 2024",
		"2024 Q3 earnings":                         "Q3 2024",
		"third quarter of fiscal 2024":             "Q3 2024",
		"for the quarter ended September 30, 2024": "Q3 2024",
		"FY2024 annual report":                     "FY2024",
		"fiscal year 2023 results":                 "FY2023",
		"first half of 2025":                       "H1 2025",
		"H2 2024 interim":                          "H2 2024",
		"Acme revenue growth":                      "",
		"fourth quarter 2023":                      "Q4 2023",
	}
	for text, want := range cases {
		p, ok := ExtractPeriod(text)
		got := ""
		if ok {
			got = p.Label()
		}
		if got != want {
			t.Errorf("ExtractPeriod(%q) = %q, want %q", text, got, want)
		}
	}
	p, _ := ExtractPeriod("quarter ended June 30, 2024")
	if !p.Derived {
		t.Fatal("a quarter derived from a month must say so")
	}
}

func TestComparePeriods(t *testing.T) {
	q3, _ := ExtractPeriod("Q3 2024")
	q2, _ := ExtractPeriod("Q2 2024")
	fy, _ := ExtractPeriod("FY2024")
	q3b, _ := ExtractPeriod("third quarter 2024")
	if Compare(q3, q3b) != PeriodMatch || Compare(q3, q2) != PeriodMismatch || Compare(q3, fy) != PeriodRelated || Compare(q3, nil) != PeriodUnknown || Compare(nil, q3) != PeriodUnknown {
		t.Fatal("period comparison")
	}
}

func TestCoverDateNeedsLabel(t *testing.T) {
	if CoverDate("Net sales rose 4% on March 3, 2024 versus last year") != "" {
		t.Fatal("a bare date in prose is not a report date")
	}
	if CoverDate("Dated: 2024-10-30") != "2024-10-30" {
		t.Fatal("labeled ISO date")
	}
}

func TestDetectIntent(t *testing.T) {
	cases := []struct {
		q          string
		kind       string
		latest     bool
		historical bool
		period     string
	}{
		{"Acme Q3 2024 revenue", IntentFinancial, false, true, "Q3 2024"},
		{"Acme latest earnings", IntentFinancial, true, false, ""},
		{"what do people on reddit think about Acme", IntentSentiment, false, false, ""},
		{"Acme sentiment this week", IntentSentiment, true, false, ""},
		{"Acme headquarters", IntentGeneral, false, false, ""},
		{"Acme historical stock splits", IntentGeneral, false, true, ""},
	}
	for _, c := range cases {
		in := DetectIntent(c.q)
		if in.Kind != c.kind || in.Latest != c.latest || in.Historical != c.historical || in.PeriodText != c.period {
			t.Errorf("DetectIntent(%q) = %+v", c.q, in)
		}
	}
}

func cand(idx int, url, title string, published, period, state string) *Candidate {
	m := ForSearchResult(url, title, "", published, "2026-11-07T12:00:00Z", now)
	if period != "" {
		m.Period = period
	}
	if state != "" {
		m.EvidenceState = state
	}
	return &Candidate{Index: idx, URL: url, Title: title, Meta: m}
}

func order(list []*Candidate) []string {
	var out []string
	for _, c := range list {
		out = append(out, c.URL)
	}
	return out
}

func TestOfficialReportForRequestedQuarterBeatsNewerSocialPost(t *testing.T) {
	q := "Acme Q3 2024 revenue"
	reddit := cand(0, "https://www.reddit.com/r/stocks/comments/1/acme_revenue/", "Acme revenue is crazy", "2026-11-06", "", "")
	news := cand(1, "https://www.reuters.com/acme-q3", "Acme Q3 2024 revenue rises", "2024-10-31", "Q3 2024", "")
	filing := cand(2, "https://www.sec.gov/Archives/edgar/acme-10q.htm", "Acme 10-Q", "2024-10-30", "Q3 2024", "")
	wrongQ := cand(3, "https://www.sec.gov/Archives/edgar/acme-10q-q2.htm", "Acme 10-Q", "2026-08-01", "Q2 2026", "")
	for _, p := range []string{"", ProfileFactsFirst, ProfileBalanced} {
		got, _ := Rank([]*Candidate{reddit, news, filing, wrongQ}, Options{Profile: p, Intent: DetectIntent(q), Now: now})
		if got[0].URL != filing.URL {
			t.Fatalf("profile %q: the filing for the requested quarter must be first: %v", p, order(got))
		}
		if got[len(got)-1].URL == filing.URL {
			t.Fatalf("filing ranked last: %v", order(got))
		}
	}
	got, _ := Rank([]*Candidate{reddit, news, filing, wrongQ}, Options{Profile: ProfileSentimentFirst, Intent: DetectIntent(q), Now: now})
	if got[0].URL == reddit.URL {
		t.Fatalf("even sentiment-first must not put a post first for a period-specific financial question: %v", order(got))
	}
	if filing.Meta.Role != RolePrimary || reddit.Meta.Role != RoleSentiment {
		t.Fatal("the profile must never change an evidence role")
	}
}

func TestLatestOfficialFilingWins(t *testing.T) {
	old := cand(0, "https://www.sec.gov/old-10q.htm", "Acme 10-Q", "2025-05-01", "", "")
	newer := cand(1, "https://www.sec.gov/new-10q.htm", "Acme 10-Q", "2026-08-01", "", "")
	stale := cand(2, "https://www.reuters.com/acme", "Acme results", "2025-02-01", "", "")
	got, _ := Rank([]*Candidate{old, newer, stale}, Options{Intent: DetectIntent("Acme latest 10-Q revenue"), Now: now})
	if got[0].URL != newer.URL {
		t.Fatalf("latest filing must win: %v", order(got))
	}
}

func TestOldOfficialReportBeatsNewRedditForHistoricalFacts(t *testing.T) {
	filing := cand(1, "https://www.sec.gov/acme-10k-2019.htm", "Acme 10-K", "2020-02-15", "FY2019", "")
	reddit := cand(0, "https://www.reddit.com/r/investing/comments/2/acme/", "Acme 2019 revenue", "2026-11-05", "", "")
	got, _ := Rank([]*Candidate{reddit, filing}, Options{Intent: DetectIntent("Acme FY2019 revenue"), Now: now})
	if got[0].URL != filing.URL {
		t.Fatalf("%v", order(got))
	}
}

func TestRedditDiscoverableForSentimentQuestion(t *testing.T) {
	reddit := cand(2, "https://www.reddit.com/r/stocks/comments/3/acme_earnings_reaction/", "Acme earnings reaction thread", "2026-11-06T09:00:00Z", "", "")
	filing := cand(0, "https://www.sec.gov/acme-10q.htm", "Acme 10-Q", "2026-10-30", "", "")
	blog := cand(1, "https://random.example/acme", "Acme post", "2024-01-01", "", "")
	for _, p := range []string{"", ProfileBalanced, ProfileSentimentFirst} {
		got, _ := Rank([]*Candidate{filing, blog, reddit}, Options{Profile: p, Intent: DetectIntent("what do people on reddit think about Acme earnings"), Now: now})
		if got[0].URL != reddit.URL {
			t.Fatalf("profile %q: a recent thread must be first for a sentiment question: %v", p, order(got))
		}
		if reddit.Meta.Role != RoleSentiment || reddit.Meta.Type != TypeCommunity {
			t.Fatal("must stay labeled as sentiment")
		}
		if reddit.Meta.PublishedAt != "2026-11-06T09:00:00Z" {
			t.Fatalf("post timestamp lost: %s", reddit.Meta.PublishedAt)
		}
	}
}

func TestSentimentFirstDoesNotPromoteSocialForPlainFinancialQuestion(t *testing.T) {
	filing := cand(1, "https://www.sec.gov/acme-10q.htm", "Acme 10-Q", "2026-10-30", "", "")
	reddit := cand(0, "https://www.reddit.com/r/stocks/comments/4/acme/", "Acme revenue", "2026-11-06", "", "")
	got, _ := Rank([]*Candidate{reddit, filing}, Options{Profile: ProfileFactsFirst, Intent: DetectIntent("Acme latest revenue"), Now: now})
	if got[0].URL != filing.URL {
		t.Fatalf("%v", order(got))
	}
}

func TestSyndicatedDuplicatesAreOneSource(t *testing.T) {
	a := cand(0, "https://www.reuters.com/markets/acme-profit-jumps", "Acme profit jumps on cloud demand - Reuters", "2026-11-02", "", "")
	b := cand(1, "https://finance.example.com/news/acme-profit-jumps", "Acme profit jumps on cloud demand | Example Finance", "2026-11-03", "", "")
	c := cand(2, "https://www.sec.gov/acme-8k.htm", "Acme 8-K", "2026-11-01", "", "")
	got, filtered := Rank([]*Candidate{b, a, c}, Options{Intent: DetectIntent("Acme profit"), Now: now})
	if len(got) != 2 {
		t.Fatalf("duplicates must collapse: %v", order(got))
	}
	var kept *Candidate
	for _, g := range got {
		if g.Meta.Type == TypeEstablishedNews {
			kept = g
		}
	}
	if kept == nil || kept.URL != a.URL {
		t.Fatalf("the higher-authority original must be kept, got %v", order(got))
	}
	if len(kept.Meta.AlsoSeenAt) != 1 || kept.Meta.AlsoSeenAt[0] != b.URL {
		t.Fatalf("provenance of the copy lost: %v", kept.Meta.AlsoSeenAt)
	}
	if len(filtered) != 1 || !strings.Contains(filtered[0].Reason, "syndicated") {
		t.Fatalf("%v", filtered)
	}
}

func TestSnippetDuplicates(t *testing.T) {
	long := strings.Repeat("Acme reported strong quarterly results driven by cloud growth and margins ", 3)
	a := cand(0, "https://a.example/1", "One", "", "", "")
	b := cand(1, "https://b.example/2", "Two", "", "", "")
	a.Snippet, b.Snippet = long, long
	got, filtered := Dedupe([]*Candidate{a, b})
	if len(got) != 1 || len(filtered) != 1 {
		t.Fatalf("%d %v", len(got), filtered)
	}
}

func TestPeriodMismatchDemotesRecentPage(t *testing.T) {
	right := cand(1, "https://investors.acme.com/q3-2025", "Acme Q3 2025 results", "2025-10-30", "Q3 2025", "")
	wrong := cand(0, "https://investors.acme.com/q1-2026", "Acme Q1 2026 results", "2026-04-30", "Q1 2026", "")
	got, _ := Rank([]*Candidate{wrong, right}, Options{Intent: DetectIntent("Acme Q3 2025 earnings"), Now: now})
	if got[0].URL != right.URL {
		t.Fatalf("%v", order(got))
	}
	if !strings.Contains(strings.Join(wrong.Meta.WhyRanked, " "), "not the requested Q3 2025") {
		t.Fatalf("%v", wrong.Meta.WhyRanked)
	}
}

func TestWindowAndFallback(t *testing.T) {
	inWin := cand(1, "https://www.reuters.com/a", "Acme headline today", "2026-11-05", "", "")
	out := cand(0, "https://www.reuters.com/b", "Acme old headline", "2026-01-01", "", "")
	undated := cand(2, "https://blog.example/c", "Acme blog", "", "", "")
	opts := Options{Intent: DetectIntent("Acme latest news"), WindowDays: 14, Now: now}
	got, _ := Rank([]*Candidate{out, inWin, undated}, opts)
	if got[0].URL != inWin.URL {
		t.Fatalf("%v", order(got))
	}
	if len(got) != 3 {
		t.Fatal("older sources must stay as a fallback, not disappear")
	}
	why := strings.Join(out.Meta.WhyRanked, " ")
	if !strings.Contains(why, "outside the 14-day window") {
		t.Fatalf("%s", why)
	}
	if !strings.Contains(strings.Join(undated.Meta.WhyRanked, " "), "publication date unknown") {
		t.Fatalf("%v", undated.Meta.WhyRanked)
	}
}

func TestEvidenceStatesAffectRankingVisibly(t *testing.T) {
	read := cand(1, "https://www.sec.gov/a", "A", "2026-10-01", "", StatePDFText)
	denied := cand(0, "https://www.sec.gov/b", "B", "2026-10-01", "", StateAccessDenied)
	got, _ := Rank([]*Candidate{denied, read}, Options{Intent: DetectIntent("Acme filing"), Now: now})
	if got[0].URL != read.URL {
		t.Fatalf("%v", order(got))
	}
	if !strings.Contains(strings.Join(denied.Meta.WhyRanked, " "), "access denied") {
		t.Fatalf("%v", denied.Meta.WhyRanked)
	}
}

func TestLimitRecordsFiltered(t *testing.T) {
	var list []*Candidate
	for i := 0; i < 4; i++ {
		list = append(list, cand(i, "https://r"+string(rune('a'+i))+".example/x", "T"+string(rune('a'+i)), "", "", ""))
	}
	got, filtered := Rank(list, Options{Intent: DetectIntent("anything"), Limit: 2, Now: now})
	if len(got) != 2 || len(filtered) != 2 || !strings.Contains(filtered[0].Reason, "below the top 2") {
		t.Fatalf("%d %v", len(got), filtered)
	}
	for i, g := range got {
		if g.Meta.Rank != i+1 {
			t.Fatalf("rank %d", g.Meta.Rank)
		}
	}
}

func TestDescribe(t *testing.T) {
	d := DetectIntent("Acme Q3 2024 revenue").Describe("", 0)
	if !strings.Contains(d, "Q3 2024") || !strings.Contains(d, "facts-first") {
		t.Fatal(d)
	}
	if !strings.Contains(DetectIntent("Acme latest").Describe(ProfileBalanced, 30), "window 30 days") {
		t.Fatal("window not described")
	}
}

func TestForFiling(t *testing.T) {
	m := ForFiling("10-Q", "2026-08-01", "2026-11-07T12:00:00Z", now)
	if m.Type != TypeOfficialFiling || m.PublishedAt != "2026-08-01" || m.DateSource != DateFilingIndex || m.EvidenceState != StatePageRead {
		t.Fatalf("%+v", m)
	}
}
