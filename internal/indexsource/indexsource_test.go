package indexsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

var (
	usSpec = Spec{ID: "index_test_us", Name: "Test US index", Country: "US", Nominal: 4}
	seSpec = Spec{ID: "index_test_se", Name: "Test SE index", Country: "SE", Nominal: 3}
	meta   = Meta{AsOf: "2026-11-01", SourceName: "My export", LicenseTerms: "I hold a licence for this file.", LicenseConfirm: true}
)

func TestParseCSVHappyPath(t *testing.T) {
	csv := "\xef\xbb\xbfSymbol,Name,Weight,Exchange\nAAA,Alpha,25.5,\nBBB,,,\nBRK.B,Berkshire,%,\nCCC,Gamma,10%,\n"
	// A bare "%" weight is invalid, not 0.
	if _, problems, err := ParseCSV([]byte(csv), usSpec, meta, now); err == nil || !strings.Contains(strings.Join(problems, "|"), "weight") {
		t.Fatalf("a malformed weight must be refused, got %v", problems)
	}
	csv = "Symbol,Name,Weight,Exchange\nAAA,Alpha,25.5,\nBBB,,,\nBRK.B,Berkshire,,\nCCC,Gamma,10%,\n"
	snap, problems, err := ParseCSV([]byte(csv), usSpec, meta, now)
	if err != nil {
		t.Fatalf("refused: %v %v", err, problems)
	}
	if len(snap.Members) != 4 || snap.Members[2].Symbol != "BRK-B" {
		t.Fatalf("members/class share: %+v", snap.Members)
	}
	if snap.Members[1].Weight != nil || snap.Members[0].Weight == nil || *snap.Members[3].Weight != 10 {
		t.Errorf("a blank weight must stay nil, never 0: %+v", snap.Members)
	}
	if snap.PointInTime || snap.Provider != ProviderCSV || snap.AsOf != "2026-11-01" || snap.LicenseTerms == "" {
		t.Errorf("snapshot: %+v", snap)
	}
	if got, known := snap.MembersAt("2020-01-01"); known || len(got) != 4 {
		t.Errorf("a CSV has no past membership: known=%v", known)
	}
	if !strings.Contains(strings.Join(snap.Warnings, " "), "survivorship") {
		t.Errorf("survivorship warning missing: %v", snap.Warnings)
	}
}

func TestParseCSVRefusals(t *testing.T) {
	good := "symbol\nAAA\nBBB\nCCC\nDDD\n"
	cases := map[string]struct {
		csv  string
		spec Spec
		meta func(*Meta)
		want string
	}{
		"duplicate":        {"symbol\nAAA\nBBB\nAAA\nCCC\nDDD\n", usSpec, nil, "appears twice"},
		"duplicate by dot": {"symbol\nBRK.B\nBRK-B\nCCC\nDDD\n", usSpec, nil, "appears twice"},
		"no symbol column": {"ticker\nAAA\n", usSpec, nil, "symbol column"},
		"empty":            {"", usSpec, nil, "no header"},
		"no rows":          {"symbol\n", usSpec, nil, "no rows"},
		"far too few":      {"symbol\nAAA\n", usSpec, nil, "wrong list"},
		"far too many":     {"symbol\n" + strings.Repeat("X\n", 3), Spec{ID: "i", Name: "n", Country: "US", Nominal: 1}, nil, "appears twice"},
		"empty symbol":     {"symbol,name\n,Nameless\nAAA,a\nBBB,b\nCCC,c\nDDD,d\n", usSpec, nil, "empty"},
		"bad weight":       {"symbol,weight\nAAA,abc\nBBB,1\nCCC,1\nDDD,1\n", usSpec, nil, "weight"},
		"weight over 100":  {"symbol,weight\nAAA,101\nBBB,1\nCCC,1\nDDD,1\n", usSpec, nil, "weight"},
		"no licence":       {good, usSpec, func(m *Meta) { m.LicenseConfirm = false }, "confirm"},
		"short terms":      {good, usSpec, func(m *Meta) { m.LicenseTerms = "ok" }, "10 to 1000"},
		"no source":        {good, usSpec, func(m *Meta) { m.SourceName = " " }, "source name"},
		"bad as_of":        {good, usSpec, func(m *Meta) { m.AsOf = "11/01/2026" }, "YYYY-MM-DD"},
		"future as_of":     {good, usSpec, func(m *Meta) { m.AsOf = "2026-12-01" }, "future"},
		"se without exch":  {"symbol\nVOLV-B\nERIC-B\nABB\n", seSpec, nil, "no exchange"},
		"se wrong suffix":  {"symbol\nVOLV-B.ST\nERIC-B.CO\nABB.ST\n", seSpec, nil, "suffix"},
		"se wrong exch":    {"symbol,exchange\nVOLV-B,XNAS\nERIC-B,ST\nABB,ST\n", seSpec, nil, "not one of"},
		"us with se exch":  {"symbol,exchange\nAAA,ST\nBBB,\nCCC,\nDDD,\n", usSpec, nil, "not a US exchange"},
		"bad symbol chars": {"symbol\nAA$A\nBBB\nCCC\nDDD\n", usSpec, nil, "not a valid symbol"},
	}
	for name, c := range cases {
		m := meta
		if c.meta != nil {
			c.meta(&m)
		}
		snap, problems, err := ParseCSV([]byte(c.csv), c.spec, m, now)
		if err == nil || len(snap.Members) != 0 {
			t.Errorf("%s: expected a refusal, got %+v", name, snap)
			continue
		}
		if !strings.Contains(strings.Join(problems, "|"), c.want) {
			t.Errorf("%s: %v does not mention %q", name, problems, c.want)
		}
	}
}

func TestParseCSVSwedishSuffixHandling(t *testing.T) {
	csv := "symbol,name,exchange\nVOLV B,Volvo B,XSTO\nERIC-B.ST,Ericsson B,\nABB,ABB,Nasdaq Stockholm\n"
	snap, problems, err := ParseCSV([]byte(csv), seSpec, meta, now)
	if err != nil {
		t.Fatalf("%v %v", err, problems)
	}
	want := []string{"VOLV-B.ST", "ERIC-B.ST", "ABB.ST"}
	for i, s := range snap.Symbols() {
		if s != want[i] {
			t.Errorf("symbol %d: %s, want %s", i, s, want[i])
		}
	}
}

func TestCountWarningWhenNearNominal(t *testing.T) {
	snap, problems, err := ParseCSV([]byte("symbol\nAAA\nBBB\nCCC\n"), usSpec, meta, now)
	if err != nil {
		t.Fatalf("%v %v", err, problems)
	}
	if !strings.Contains(strings.Join(snap.Warnings, " "), "the index name says 4") {
		t.Errorf("expected a count warning: %v", snap.Warnings)
	}
}

func TestMembersAtPointInTime(t *testing.T) {
	s := Snapshot{AsOf: "2026-11-01", PointInTime: true, HistoryFrom: "2020-01-01",
		Members: []Member{{Symbol: "AAA"}, {Symbol: "NEW"}, {Symbol: "NEW2"}},
		Changes: []Change{
			{Date: "2020-01-01", Added: "AAA", Removed: "OLD0"},
			{Date: "2022-06-01", Added: "NEW", Removed: "OLD1"},
			{Date: "2025-03-03", Added: "NEW2", Removed: "OLD2"},
			{Date: "2025-03-03", Removed: "ONLYOUT"},
		}}
	check := func(date string, want string, known bool) {
		t.Helper()
		got, k := s.MembersAt(date)
		if k != known || strings.Join(got, ",") != want {
			t.Errorf("%s: got %v known=%v, want %s known=%v", date, got, k, want, known)
		}
	}
	check("2026-11-01", "AAA,NEW,NEW2", true) // today
	check("2027-01-01", "AAA,NEW,NEW2", true) // after as-of: today's list
	check("2025-03-03", "AAA,NEW,NEW2", true) // changes on the date are in effect
	check("2025-03-02", "AAA,NEW,OLD2,ONLYOUT", true)
	check("2022-06-01", "AAA,NEW,OLD2,ONLYOUT", true)
	check("2022-05-31", "AAA,OLD1,OLD2,ONLYOUT", true)
	check("2020-01-01", "AAA,OLD1,OLD2,ONLYOUT", true)
	check("2019-12-31", "AAA,NEW,NEW2", false) // before the history: fallback, not known

	ever := s.EverMembers("2022-01-01")
	if strings.Join(ever, ",") != "AAA,NEW,NEW2,OLD1,OLD2,ONLYOUT" {
		t.Errorf("ever members: %v", ever)
	}
	s.PointInTime = false
	if got, known := s.MembersAt("2023-01-01"); known || len(got) != 3 {
		t.Errorf("without history nothing is known: %v %v", got, known)
	}
}

// fakeFMP serves fixtures by path and records every request URL.
type fakeFMP struct {
	routes map[string]string
	status map[string]int
	urls   []string
	err    error
}

func (f *fakeFMP) Do(r *http.Request) (*http.Response, error) {
	f.urls = append(f.urls, r.URL.String())
	if f.err != nil {
		return nil, &urlError{r.URL.String(), f.err}
	}
	name := strings.TrimPrefix(r.URL.Path, "/stable/")
	status := http.StatusOK
	if s, ok := f.status[name]; ok {
		status = s
	}
	body, ok := f.routes[name]
	if !ok {
		status, body = http.StatusNotFound, `{}`
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

type urlError struct {
	url string
	err error
}

func (e *urlError) Error() string { return "Get " + e.url + ": " + e.err.Error() }

func currentFixture(n int) string {
	var parts []string
	for i := 0; i < n; i++ {
		parts = append(parts, fmt.Sprintf(`{"symbol":"T%03d","name":"Company %d","sector":"X"}`, i, i))
	}
	parts[0] = `{"symbol":"BRK.B","name":"Berkshire"}`
	return "[" + strings.Join(parts, ",") + "]"
}

const historyFixture = `[
 {"dateAdded":"November 26, 2024","addedSecurity":"New Co","symbol":"T001","removedTicker":"GONE","removedSecurity":"Gone Inc","date":"2024-11-26","reason":"Market cap change"},
 {"dateAdded":"March 3, 2021","symbol":"T002","removedTicker":"OLDX","date":"2021-03-03"},
 {"symbol":"","removedTicker":"","date":"2020-01-01"},
 {"symbol":"T003","removedTicker":"","dateAdded":"not a date"}
]`

var fmpSpec = Spec{ID: "index_sp500", Name: "S&P 500", Country: "US", Nominal: 500}

func TestFMPFetchCurrentAndHistory(t *testing.T) {
	f := &fakeFMP{routes: map[string]string{"sp500-constituent": currentFixture(503), "historical-sp500-constituent": historyFixture}}
	snap, problems, err := FMP{Client: f, Key: "SECRETKEY"}.Fetch(context.Background(), fmpSpec, meta, now)
	if err != nil {
		t.Fatalf("%v %v", err, problems)
	}
	if len(snap.Members) != 503 || snap.Members[0].Symbol != "BRK-B" || snap.AsOf != "2026-11-06" || snap.Provider != ProviderFMP {
		t.Fatalf("snapshot: %d members, first %+v, as of %s", len(snap.Members), snap.Members[0], snap.AsOf)
	}
	if !snap.PointInTime || snap.HistoryFrom != "2021-03-03" || len(snap.Changes) != 2 {
		t.Errorf("history: %+v from %s", snap.Changes, snap.HistoryFrom)
	}
	if got, known := snap.MembersAt("2024-11-25"); !known || !contains(got, "GONE") || contains(got, "T001") {
		t.Errorf("point in time lookup: known=%v gone=%v", known, contains(got, "GONE"))
	}
	if snap.Unverified {
		t.Errorf("S&P 500 endpoints are documented; the list is not flagged")
	}
	if len(f.urls) != 2 || !strings.HasPrefix(f.urls[0], "https://financialmodelingprep.com/stable/sp500-constituent?apikey=") {
		t.Errorf("urls: %v", f.urls)
	}
	// Nothing returned may hold the key.
	if strings.Contains(fmt.Sprint(snap), "SECRETKEY") {
		t.Error("the key leaked into the snapshot")
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

func TestFMPNasdaqIsFlaggedUnverified(t *testing.T) {
	f := &fakeFMP{routes: map[string]string{"nasdaq-constituent": currentFixture(101), "historical-nasdaq-constituent": historyFixture}}
	spec := Spec{ID: "index_nasdaq100", Name: "Nasdaq-100", Country: "US", Nominal: 100}
	snap, problems, err := FMP{Client: f, Key: "k"}.Fetch(context.Background(), spec, meta, now)
	if err != nil || !snap.Unverified || snap.UnverifiedNote == "" {
		t.Fatalf("expected an unverified list: %v %v %+v", err, problems, snap.Unverified)
	}
}

func TestFMPFailuresAreSafe(t *testing.T) {
	ctx := context.Background()
	// OMXS30 has no FMP endpoint.
	if _, _, err := (FMP{Client: &fakeFMP{}, Key: "k"}).Fetch(ctx, seSpec, meta, now); err == nil {
		t.Error("OMXS30 must be CSV only")
	}
	// Licence acknowledgement is required before any request.
	f := &fakeFMP{routes: map[string]string{}}
	m := meta
	m.LicenseConfirm = false
	if _, _, err := (FMP{Client: f, Key: "k"}).Fetch(ctx, fmpSpec, m, now); err == nil || len(f.urls) != 0 {
		t.Errorf("unconfirmed licence must not call out: %v %v", err, f.urls)
	}
	// No key.
	if _, _, err := (FMP{Client: f}).Fetch(ctx, fmpSpec, meta, now); err == nil || len(f.urls) != 0 {
		t.Error("missing key")
	}
	// Auth failure, plan error as 200 JSON object, rate limit.
	for name, f := range map[string]*fakeFMP{
		"401":  {routes: map[string]string{"sp500-constituent": `{}`}, status: map[string]int{"sp500-constituent": 401}},
		"200e": {routes: map[string]string{"sp500-constituent": `{"Error Message":"Invalid API KEY. SECRETKEY"}`}},
		"402":  {routes: map[string]string{"sp500-constituent": `{}`}, status: map[string]int{"sp500-constituent": 402}},
	} {
		_, problems, err := FMP{Client: f, Key: "SECRETKEY"}.Fetch(ctx, fmpSpec, meta, now)
		if !errors.Is(err, ErrFMPAuth) || strings.Contains(strings.Join(problems, " ")+err.Error(), "SECRETKEY") {
			t.Errorf("%s: %v %v", name, err, problems)
		}
	}
	f = &fakeFMP{routes: map[string]string{"sp500-constituent": `{}`}, status: map[string]int{"sp500-constituent": 429}}
	if _, _, err := (FMP{Client: f, Key: "k"}).Fetch(ctx, fmpSpec, meta, now); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("rate limit: %v", err)
	}
	// A transport error carries the URL, which carries the key: it must be scrubbed.
	f = &fakeFMP{err: errors.New("dial failed for apikey=SECRETKEY")}
	_, problems, err := FMP{Client: f, Key: "SECRETKEY"}.Fetch(ctx, fmpSpec, meta, now)
	if err == nil || strings.Contains(err.Error(), "SECRETKEY") || strings.Contains(strings.Join(problems, " "), "SECRETKEY") {
		t.Errorf("the key leaked through a transport error: %v %v", err, problems)
	}
	// Unknown shape: no symbol field anywhere.
	f = &fakeFMP{routes: map[string]string{"sp500-constituent": `[{"ticker":"AAA"},{"ticker":"BBB"}]`}}
	if _, _, err := (FMP{Client: f, Key: "k"}).Fetch(ctx, fmpSpec, meta, now); err == nil {
		t.Error("an unrecognized shape must be refused, not guessed")
	}
	// Far too few members: wrong data.
	f = &fakeFMP{routes: map[string]string{"sp500-constituent": currentFixture(20)}}
	if _, _, err := (FMP{Client: f, Key: "k"}).Fetch(ctx, fmpSpec, meta, now); err == nil {
		t.Error("20 members is not the S&P 500")
	}
}

func TestFMPHistoryFailureKeepsCurrentWithWarning(t *testing.T) {
	f := &fakeFMP{routes: map[string]string{"sp500-constituent": currentFixture(500)}, status: map[string]int{"historical-sp500-constituent": 402}}
	snap, problems, err := FMP{Client: f, Key: "k"}.Fetch(context.Background(), fmpSpec, meta, now)
	if err != nil {
		t.Fatalf("%v %v", err, problems)
	}
	if snap.PointInTime || !strings.Contains(strings.Join(snap.Warnings, " "), "Past membership could not be fetched") {
		t.Errorf("history failure must be disclosed: %v", snap.Warnings)
	}
}
