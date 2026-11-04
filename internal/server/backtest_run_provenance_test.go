package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"floweysales/internal/provenance"
	"floweysales/internal/storage"
)

func TestSavedRunProvenanceNamesMethodProviderAndDates(t *testing.T) {
	run := storage.BacktestRun{ID: 7, Kind: "single", Result: `{"provider":"stooq","generated_at":"2026-03-02T10:00:00Z","data_end":"2026-02-27"}`}
	f := backtestRunProvenance(run)
	if f.Kind != provenance.Calculated || f.Status != provenance.Known || f.Provider != "stooq" {
		t.Fatalf("fact: %+v", f)
	}
	if f.Method == nil || f.Method.ID != "backtest.single" || f.Method.Version != backtestMethodVersion || f.Method.Formula == "" {
		t.Errorf("method: %+v", f.Method)
	}
	if f.AsOf == nil || f.AsOf.Format("2006-01-02") != "2026-02-27" || f.RetrievedAt == nil || f.RetrievedAt.Format("2006-01-02") != "2026-03-02" {
		t.Errorf("dates: asof %v retrieved %v", f.AsOf, f.RetrievedAt)
	}
	if f.RunID != "backtest_run:7" || !strings.Contains(f.Note, "survivorship") {
		t.Errorf("run id / note: %q %q", f.RunID, f.Note)
	}
}

func TestSavedRunProvenanceIsPartialWhenTheResultDoesNotRecordAProvider(t *testing.T) {
	f := backtestRunProvenance(storage.BacktestRun{ID: 1, Kind: "batch", Result: `{"generated_at":"2026-03-02T10:00:00Z"}`})
	if f.Status != provenance.Partial || f.Provider != "" || f.AsOf != nil || !strings.Contains(f.Note, "does not record a provider") {
		t.Errorf("a missing provider must be disclosed, not invented: %+v", f)
	}
}

func TestSavedRunProvenanceClaimsNothingForUnreadableResultsOrKinds(t *testing.T) {
	for _, run := range []storage.BacktestRun{
		{ID: 1, Kind: "batch", Result: `not json`},
		{ID: 2, Kind: "mystery", Result: `{"provider":"stooq","generated_at":"2026-03-02T10:00:00Z"}`},
	} {
		f := backtestRunProvenance(run)
		if f.Status != provenance.Unknown || f.Method != nil || f.Provider != "" || f.RetrievedAt != nil {
			t.Errorf("run %d should be unknown: %+v", run.ID, f)
		}
		if err := f.Validate(); err != nil {
			t.Errorf("invalid fact: %v", err)
		}
	}
}

func TestSavedRunProvenanceDatesAreUTCAndMissingOnesDisclosed(t *testing.T) {
	f := backtestRunProvenance(storage.BacktestRun{ID: 3, Kind: "single", Result: `{"provider":"stooq","generated_at":"2026-03-02T01:30:00+02:00","data_end":"31/12/2025"}`})
	if f.RetrievedAt == nil || f.RetrievedAt.Format(time.RFC3339) != "2026-03-01T23:30:00Z" {
		t.Errorf("an offset time must be converted to UTC: %v", f.RetrievedAt)
	}
	if f.AsOf != nil {
		t.Errorf("a malformed date must not become a date: %v", f.AsOf)
	}
	g := backtestRunProvenance(storage.BacktestRun{ID: 4, Kind: "single", Result: `{"provider":"stooq"}`})
	if g.Status != provenance.Partial || g.RetrievedAt != nil || !strings.Contains(g.Note, "not recorded") {
		t.Errorf("a missing production time makes the fact partial and says so: %+v", g)
	}
}

func TestSavedRunAndItsJSONExportCarryProvenance(t *testing.T) {
	e := runsEngine(t)
	rec := runCall(e, http.MethodPost, "/api/market/backtest/runs", `{"name":"One","kind":"single","query":"?symbols=UP,FLAT&strategy=momentum&top=1&years=3"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/api/market/backtest/runs/1", "/api/market/backtest/runs/1/export"} {
		got := runCall(e, http.MethodGet, path, "")
		var doc struct {
			Provenance provenance.Fact `json:"provenance"`
		}
		if err := json.Unmarshal(got.Body.Bytes(), &doc); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if doc.Provenance.Status != provenance.Known || doc.Provenance.Validate() != nil || doc.Provenance.RetrievedAt == nil || doc.Provenance.Method == nil || doc.Provenance.Method.ID != "backtest.single" || doc.Provenance.Provider == "" || doc.Provenance.RunID != "backtest_run:1" {
			t.Errorf("%s provenance: %s", path, got.Body)
		}
	}
}
