package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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
	if f.Status != provenance.Partial || f.Provider != "" || f.AsOf != nil || !strings.Contains(f.Note, "provider is not recorded") {
		t.Errorf("a missing provider must be disclosed, not invented: %+v", f)
	}
	bad := backtestRunProvenance(storage.BacktestRun{ID: 1, Kind: "batch", Result: `not json`})
	if bad.Status != provenance.Partial || bad.RetrievedAt != nil {
		t.Errorf("an unreadable result claims nothing: %+v", bad)
	}
	if err := bad.Validate(); err != nil {
		t.Errorf("invalid fact: %v", err)
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
		if doc.Provenance.Method == nil || doc.Provenance.Method.ID != "backtest.single" || doc.Provenance.Provider == "" || doc.Provenance.RunID != "backtest_run:1" {
			t.Errorf("%s provenance: %s", path, got.Body)
		}
	}
}
