package sources

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"floweysales/internal/sourcetrust"
)

func pinTrustClock(t *testing.T) {
	t.Helper()
	old := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = old })
}

const trustSearch = `{"provider":"brave","retrieved_at_utc":"2026-11-07T12:00:00Z","results":[
{"title":"Acme thread","url":"https://www.reddit.com/r/stocks/comments/1/acme/","snippet":"revenue was 9 billion","published_at":"2026-11-06T09:00:00Z","type":"web_result"},
{"title":"Acme 10-Q","url":"https://investors.acme.com/q3.pdf","snippet":"Q3 2024","type":"web_result"}]}`

func TestRegisterSearchClassifiesWithoutRendererMetadata(t *testing.T) {
	pinTrustClock(t)
	r := NewRegistry()
	_, added := r.Register("search_web", "c1", `{"query":"acme"}`, trustSearch)
	if len(added) != 2 {
		t.Fatal(len(added))
	}
	if added[0].Trust.Role != sourcetrust.RoleSentiment || added[0].PublishedAt != "2026-11-06T09:00:00Z" {
		t.Fatalf("%+v", added[0].Trust)
	}
	if added[1].Trust.Type != sourcetrust.TypeCompanyIR || added[1].PublishedAt != "" || added[1].Trust.DateConfidence != sourcetrust.ConfNone {
		t.Fatalf("unknown date must stay unknown: %+v", added[1])
	}
}

func TestReadMergesDatesAndKeepsFileDatesApart(t *testing.T) {
	pinTrustClock(t)
	r := NewRegistry()
	r.Register("search_web", "c1", `{"query":"acme"}`, trustSearch)
	page := `{"title":"Acme Q3","text":"Interim report Q3 2024. Net sales 5","retrieved_at":"2026-11-07T12:01:00Z","read_meta":{"published_at":"October 30, 2024","date_source":"pdf_cover","date_confidence":"medium","file_created_at":"2026-10-01T00:00:00Z","http_last_modified":"Tue, 03 Nov 2026 10:00:00 GMT","period":"Q3 2024","period_source":"document_text","evidence_state":"pdf_text"}}`
	out, added := r.Register("read_source", "c2", `{"source_id":"S2"}`, page)
	if len(added) != 1 || !strings.Contains(out, `"source_id":"S2"`) {
		t.Fatal(out)
	}
	tr := added[0].Trust
	if tr.PublishedAt != "October 30, 2024" || tr.FileCreatedAt != "2026-10-01T00:00:00Z" || tr.HTTPLastModified == "" || tr.Period != "Q3 2024" || tr.EvidenceState != sourcetrust.StatePDFText {
		t.Fatalf("%+v", tr)
	}
	if tr.PublishedAt == tr.FileCreatedAt || tr.PublishedAt == tr.RetrievedAt {
		t.Fatal("file or retrieval date used as report date")
	}
	if added[0].PublishedAt != "October 30, 2024" {
		t.Fatal("published_at must follow trust")
	}
}

func TestBlockedReadIsAccessDeniedNotRead(t *testing.T) {
	pinTrustClock(t)
	r := NewRegistry()
	r.Register("search_web", "c1", `{"query":"acme"}`, trustSearch)
	got, ok := r.RecordReadFailure(`{"source_id":"S2"}`, "HTTP status 403: the site is denying automated page access")
	if !ok || got.Trust.EvidenceState != sourcetrust.StateAccessDenied || got.Access != AccessListed {
		t.Fatalf("%+v", got)
	}
	got, _ = r.RecordReadFailure(`{"source_id":"S1"}`, "the page has no readable text")
	if got.Trust.EvidenceState != sourcetrust.StateReadFailed {
		t.Fatalf("%+v", got.Trust)
	}
	if _, ok := r.RecordReadFailure(`{"source_id":"S9"}`, "x"); ok {
		t.Fatal("unknown source")
	}
}

func TestVisionDerivedStateSurvivesRead(t *testing.T) {
	pinTrustClock(t)
	r := NewRegistry()
	r.Register("search_web", "c1", `{"query":"acme"}`, trustSearch)
	page := `{"title":"t","text":"scanned report","retrieved_at":"2026-11-07T12:01:00Z","read_meta":{"evidence_state":"vision_derived"}}`
	_, added := r.Register("read_source", "c2", `{"source_id":"S2"}`, page)
	if added[0].Trust.EvidenceState != sourcetrust.StateVisionDerived {
		t.Fatalf("%+v", added[0].Trust)
	}
}

func TestSecSectionIsPrimaryWithFilingDate(t *testing.T) {
	pinTrustClock(t)
	r := NewRegistry()
	_, added := r.Register("get_sec_section", "c1", "", `{"form":"10-Q","item":"Item 2","accession":"0001","filed":"2026-08-01","document_url":"https://www.sec.gov/x.htm","excerpt":"Revenue 5","retrieved_at":"2026-11-07T12:00:00Z"}`)
	tr := added[0].Trust
	if tr.Role != sourcetrust.RolePrimary || tr.DateSource != sourcetrust.DateFilingIndex || tr.EvidenceState != sourcetrust.StatePageRead {
		t.Fatalf("%+v", tr)
	}
}

func TestSentimentSourceCannotStandAloneForFinancialFact(t *testing.T) {
	pinTrustClock(t)
	r := NewRegistry()
	r.Register("search_web", "c1", `{"query":"acme"}`, trustSearch)
	found := Audit("Acme revenue was 9 billion [S1].", r)
	var hit bool
	for _, f := range found {
		hit = hit || f.Kind == SentimentAsFact
	}
	if !hit {
		t.Fatalf("%+v", found)
	}
	for _, ok := range []string{"Redditors argue revenue was 9 billion [S1].", "Acme revenue was 9 billion [S1][S2]."} {
		for _, f := range Audit(ok, r) {
			if f.Kind == SentimentAsFact {
				t.Fatalf("%q should not be flagged", ok)
			}
		}
	}
}

func TestTrustSurvivesJSONRoundTrip(t *testing.T) {
	pinTrustClock(t)
	r := NewRegistry()
	r.Register("search_web", "c1", `{"query":"acme"}`, trustSearch)
	raw, _ := json.Marshal(r.All())
	var back []Source
	if err := json.Unmarshal(raw, &back); err != nil || back[0].Trust == nil || back[0].Trust.Role != sourcetrust.RoleSentiment {
		t.Fatalf("%v %s", err, raw)
	}
	if len(RegistryFrom(back).All()) != 2 {
		t.Fatal("resume")
	}
}

func TestPublishedDateWithTextMonthIsUnderstoodByRecencyLimit(t *testing.T) {
	s := Source{PublishedAt: "October 30, 2024"}
	if (Options{MaxAgeDays: 30, Now: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)}).ageProblem(s) == "" {
		t.Fatal("an old dated source must be flagged")
	}
}
