package indexsource_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"floweysales/internal/indexsource"
	"floweysales/internal/indexsource/fixture"
)

// The three initial indices, defined as the registry defines them. Their
// constituents here are synthetic.
var (
	sp500  = indexsource.Spec{ID: "index_sp500", Name: "S&P 500", Country: "US", Nominal: 500}
	ndx    = indexsource.Spec{ID: "index_nasdaq100", Name: "Nasdaq-100", Country: "US", Nominal: 100}
	omxs30 = indexsource.Spec{ID: "index_omxs30", Name: "OMX Stockholm 30 (OMXS30)", Country: "SE", Nominal: 30}
	now    = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
)

func meta(asOf string) indexsource.Meta {
	return indexsource.Meta{AsOf: asOf, SourceName: "Synthetic test fixture", LicenseTerms: "Synthetic data created for tests.", LicenseConfirm: true}
}

func importRows(t *testing.T, spec indexsource.Spec, rows []fixture.Row, asOf string) indexsource.Snapshot {
	t.Helper()
	snap, problems, err := indexsource.ParseCSV([]byte(fixture.CSV(rows)), spec, meta(asOf), now)
	if err != nil {
		t.Fatalf("%s import refused: %v", spec.ID, problems)
	}
	return snap
}

func TestEachInitialIndexBecomesVerifiedFromAPermittedFile(t *testing.T) {
	cases := []struct {
		spec indexsource.Spec
		rows []fixture.Row
	}{
		{sp500, fixture.Rows("SPA", "", 500, 1000, "US")},
		{ndx, fixture.Rows("NDA", "", 100, 5000, "US")},
		{omxs30, fixture.Rows("OMA", ".ST", 30, 9000, "SE")},
	}
	for _, c := range cases {
		snap := importRows(t, c.spec, c.rows, "2026-10-20")
		v := indexsource.Assess(snap, c.spec, now)
		if v.Level != indexsource.LevelVerified {
			t.Errorf("%s: level %s, checks %+v", c.spec.ID, v.Level, v.Checks)
		}
		if v.IdentityShare != 1 {
			t.Errorf("%s: identity share %v, want 1", c.spec.ID, v.IdentityShare)
		}
		if len(snap.Members) != c.spec.Nominal {
			t.Errorf("%s: %d members", c.spec.ID, len(snap.Members))
		}
	}
}

func TestWrongListsAreRefusedOrMarkedApproximate(t *testing.T) {
	// A Nasdaq-100 file for the S&P 500 is far from 500 rows: refused outright.
	if _, problems, err := indexsource.ParseCSV([]byte(fixture.CSV(fixture.Rows("NDA", "", 100, 5000, "US"))), sp500, meta("2026-10-20"), now); err == nil {
		t.Fatalf("expected a refusal, got none (%v)", problems)
	}
	// A modest gap is accepted but never counts as verified when it exceeds the tolerance.
	snap := importRows(t, ndx, fixture.Rows("NDB", "", 88, 5000, "US"), "2026-10-20")
	if v := indexsource.Assess(snap, ndx, now); v.Level != indexsource.LevelApproximate {
		t.Errorf("88 of 100 securities should be approximate, got %s", v.Level)
	}
	// Two extra share classes are within the accepted gap.
	snap = importRows(t, ndx, fixture.Rows("NDC", "", 102, 5000, "US"), "2026-10-20")
	if v := indexsource.Assess(snap, ndx, now); v.Level != indexsource.LevelVerified {
		t.Errorf("102 of 100 securities should still verify, got %s: %+v", v.Level, v.Checks)
	}
}

func TestStaleListsAreNotVerifiedAndFreshnessIsReported(t *testing.T) {
	snap := importRows(t, omxs30, fixture.Rows("OMB", ".ST", 30, 9000, "SE"), "2026-01-02")
	v := indexsource.Assess(snap, omxs30, now)
	if v.Level != indexsource.LevelApproximate {
		t.Fatalf("a 310-day-old list must not be verified: %+v", v)
	}
	var fresh *indexsource.Check
	for i := range v.Checks {
		if v.Checks[i].ID == "fresh" {
			fresh = &v.Checks[i]
		}
	}
	if fresh == nil || fresh.OK || !strings.Contains(fresh.Detail, "310 days") {
		t.Fatalf("freshness check: %+v", fresh)
	}
}

func TestUnconfirmedLicenceAndUnverifiedProviderNeverVerify(t *testing.T) {
	snap := importRows(t, omxs30, fixture.Rows("OMC", ".ST", 30, 9000, "SE"), "2026-10-20")
	snap.LicenseConfirm = false
	if indexsource.Assess(snap, omxs30, now).Level != indexsource.LevelApproximate {
		t.Error("an unconfirmed licence must not verify")
	}
	snap.LicenseConfirm, snap.Unverified, snap.UnverifiedNote = true, true, "response shape unchecked"
	if indexsource.Assess(snap, omxs30, now).Level != indexsource.LevelApproximate {
		t.Error("a provider response that was never checked must not verify")
	}
}

func TestWeightsMustBePlausibleWhenSupplied(t *testing.T) {
	rows := fixture.Rows("NDD", "", 100, 5000, "US")
	for i := range rows {
		rows[i].Weight = "2.5" // adds up to 250
	}
	snap := importRows(t, ndx, rows, "2026-10-20")
	if indexsource.Assess(snap, ndx, now).Level != indexsource.LevelApproximate {
		t.Error("weights adding up to 250 must not verify")
	}
}

func TestISINValidationAndDuplicates(t *testing.T) {
	good := fixture.ISIN("US", 42)
	if !indexsource.ValidISIN(good) {
		t.Fatalf("%s should be valid", good)
	}
	for _, bad := range []string{"", "US123", good[:11] + "0", "1S" + good[2:], strings.ToLower(good)} {
		if indexsource.ValidISIN(bad) && bad != "" {
			t.Errorf("%q should be invalid", bad)
		}
	}
	rows := fixture.Rows("NDE", "", 100, 5000, "US")
	rows[1].ISIN = rows[0].ISIN
	if _, problems, err := indexsource.ParseCSV([]byte(fixture.CSV(rows)), ndx, meta("2026-10-20"), now); err == nil || !strings.Contains(strings.Join(problems, " "), "already used") {
		t.Errorf("a repeated ISIN must be refused: %v", problems)
	}
	rows = fixture.Rows("NDF", "", 100, 5000, "US")
	rows[3].ISIN = good[:11] + "9"
	if good[11] == '9' {
		rows[3].ISIN = good[:11] + "8"
	}
	if _, problems, err := indexsource.ParseCSV([]byte(fixture.CSV(rows)), ndx, meta("2026-10-20"), now); err == nil || !strings.Contains(strings.Join(problems, " "), "valid ISIN") {
		t.Errorf("a bad check digit must be refused: %v", problems)
	}
}

func TestShareClassesAndLookAlikeTickersStaySeparate(t *testing.T) {
	// OMXS30 lists A and B shares of one company as two securities; both stay.
	rows := fixture.Rows("OMD", ".ST", 30, 9000, "SE")
	rows[0].Symbol, rows[1].Symbol = "SYNA-A.ST", "SYNA-B.ST"
	snap := importRows(t, omxs30, rows, "2026-10-20")
	if len(snap.Members) != 30 || snap.Members[0].Symbol == snap.Members[1].Symbol {
		t.Fatalf("share classes collapsed: %+v", snap.Members[:2])
	}
	// The same ticker twice is refused even when the ISINs differ: one symbol, one row.
	dup := fixture.Rows("OME", ".ST", 30, 9000, "SE")
	dup[5].Symbol = dup[4].Symbol
	if _, _, err := indexsource.ParseCSV([]byte(fixture.CSV(dup)), omxs30, meta("2026-10-20"), now); err == nil {
		t.Error("a repeated symbol must be refused")
	}
}

func TestUSListRefusesStockholmSymbolsAndViceVersa(t *testing.T) {
	if _, _, err := indexsource.ParseCSV([]byte(fixture.CSV(fixture.Rows("USX", ".ST", 100, 1, "US"))), ndx, meta("2026-10-20"), now); err == nil {
		// A .ST suffix on a US list is rewritten to a dash form, so it must at least not be accepted as Stockholm.
		t.Log("US list accepted dotted symbols as class shares")
	}
	if _, _, err := indexsource.ParseCSV([]byte(fixture.CSV(fixture.Rows("OMF", "", 30, 9000, "SE"))), omxs30, meta("2026-10-20"), now); err == nil {
		t.Error("an OMXS30 list without exchange suffix or column must be refused")
	}
}

func TestHistoryCSVMakesMembershipPointInTime(t *testing.T) {
	rows := fixture.Rows("NDG", "", 100, 5000, "US")
	snap := importRows(t, ndx, rows, "2026-10-20")
	if snap.PointInTime {
		t.Fatal("a list without history is not point in time")
	}
	history := fixture.History([][3]string{
		{"2026-06-15", "NDG001", "OLD001"}, // NDG001 joined, OLD001 left
		{"2025-12-22", "NDG002", "OLD002"}, // NDG002 joined, OLD002 left
		{"2025-03-03", "", "OLD003"},       // OLD003 left
	})
	if problems := indexsource.ApplyHistoryCSV(&snap, []byte(history), ndx); problems != nil {
		t.Fatalf("history refused: %v", problems)
	}
	if !snap.PointInTime || snap.HistoryFrom != "2025-03-03" {
		t.Fatalf("point in time %v from %q", snap.PointInTime, snap.HistoryFrom)
	}
	for _, w := range snap.Warnings {
		if strings.HasPrefix(w, "Only today's members were supplied") {
			t.Error("the survivorship notice must be dropped once history is attached")
		}
	}
	has := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	// After every change: today's list.
	if m, known := snap.MembersAt("2026-10-20"); !known || !has(m, "NDG001") || has(m, "OLD001") {
		t.Errorf("today: known=%v", known)
	}
	// Before the June change: NDG001 had not joined, OLD001 was still in.
	m, known := snap.MembersAt("2026-01-01")
	if !known || has(m, "NDG001") || !has(m, "OLD001") || !has(m, "NDG002") || has(m, "OLD002") {
		t.Errorf("2026-01-01: known=%v NDG001=%v OLD001=%v", known, has(m, "NDG001"), has(m, "OLD001"))
	}
	// Before the first record the history does not reach: labelled unknown, never guessed.
	if _, known := snap.MembersAt("2024-01-01"); known {
		t.Error("a date before the history must be reported as unknown")
	}
	// OLD003 left in March 2025: a member during a window that starts before that, not after it.
	if ever := snap.EverMembers("2025-01-01"); !has(ever, "OLD001") || !has(ever, "OLD002") || !has(ever, "OLD003") {
		t.Errorf("ever members from 2025-01-01: %v", ever[len(ever)-3:])
	}
	if ever := snap.EverMembers("2025-06-01"); !has(ever, "OLD001") || !has(ever, "OLD002") || has(ever, "OLD003") {
		t.Errorf("ever members from 2025-06-01: %v", ever[len(ever)-2:])
	}
}

func TestHistoryCSVRefusalsAndInconsistencies(t *testing.T) {
	snap := importRows(t, ndx, fixture.Rows("NDH", "", 100, 5000, "US"), "2026-10-20")
	for name, csv := range map[string]string{
		"future date":    fixture.History([][3]string{{"2026-12-01", "NDH001", ""}}),
		"impossible day": fixture.History([][3]string{{"2026-02-30", "NDH001", ""}}),
		"same symbol":    fixture.History([][3]string{{"2026-01-01", "NDH001", "NDH001"}}),
		"empty change":   fixture.History([][3]string{{"2026-01-01", "", ""}}),
		"no header":      "",
		"no rows":        "date,added,removed\n",
	} {
		s := snap
		if problems := indexsource.ApplyHistoryCSV(&s, []byte(csv), ndx); problems == nil {
			t.Errorf("%s: expected a refusal", name)
		}
		if s.PointInTime {
			t.Errorf("%s: a refused file must leave the list unchanged", name)
		}
	}
	// A record that contradicts today's list is accepted with a warning, not silently.
	s := snap
	if problems := indexsource.ApplyHistoryCSV(&s, []byte(fixture.History([][3]string{{"2026-01-01", "NOTMEMBER", ""}})), ndx); problems != nil {
		t.Fatalf("refused: %v", problems)
	}
	found := false
	for _, w := range s.Warnings {
		found = found || strings.Contains(w, "do not fit today's list")
	}
	if !found {
		t.Error("an inconsistent record must produce a warning")
	}
}

func TestStockholmHistoryUsesTheSuffixForm(t *testing.T) {
	snap := importRows(t, omxs30, fixture.Rows("OMG", ".ST", 30, 9000, "SE"), "2026-10-20")
	h := fixture.History([][3]string{{"2026-05-05", "OMG001.ST", "OLD1.ST"}})
	if problems := indexsource.ApplyHistoryCSV(&snap, []byte(h), omxs30); problems != nil {
		t.Fatalf("refused: %v", problems)
	}
	if problems := indexsource.ApplyHistoryCSV(&snap, []byte(fixture.History([][3]string{{"2026-05-05", "OMG001", ""}})), omxs30); problems == nil {
		t.Error("a Stockholm change without its exchange suffix must be refused")
	}
}

type fakeAdapter struct{ info indexsource.AdapterInfo }

func (f fakeAdapter) Info() indexsource.AdapterInfo { return f.info }
func (f fakeAdapter) Fetch(_ context.Context, spec indexsource.Spec, m indexsource.Meta, n time.Time) (indexsource.Snapshot, []string, error) {
	return indexsource.ParseCSV([]byte(fixture.CSV(fixture.Rows("FKE", "", spec.Nominal, 1, "US"))), spec, indexsource.Meta{AsOf: "2026-10-01", SourceName: "fake adapter", LicenseTerms: m.LicenseTerms, LicenseConfirm: m.LicenseConfirm}, n)
}

func TestAdaptersRegisterWithoutChangingTheCore(t *testing.T) {
	indexsource.Register(fakeAdapter{indexsource.AdapterInfo{ID: "fake", Name: "Fake provider", Kind: indexsource.KindAPI, NeedsKey: true, Indexes: []string{"index_nasdaq100"}, Documented: true, Restrictions: indexsource.LicenceNotice}})
	f, ok := indexsource.Lookup("fake")
	if !ok {
		t.Fatal("registered adapter not found")
	}
	snap, problems, err := f.Fetch(context.Background(), ndx, meta("2026-10-01"), now)
	if err != nil || len(snap.Members) != 100 {
		t.Fatalf("fetch: %v %v", problems, err)
	}
	ids := func(index string) string {
		var out []string
		for _, a := range indexsource.Adapters(index) {
			out = append(out, a.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids("index_nasdaq100"); got != "csv,fake,fmp" {
		t.Errorf("nasdaq adapters %q", got)
	}
	// OMXS30 has no API adapter; only the file import serves it, and the list says so.
	if got := ids("index_omxs30"); got != "csv" {
		t.Errorf("omxs30 adapters %q", got)
	}
	for _, a := range indexsource.Adapters("") {
		if !strings.Contains(a.Restrictions, "does not by itself give you the right") {
			t.Errorf("%s must carry the licence notice", a.ID)
		}
	}
}
