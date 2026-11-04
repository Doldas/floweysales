package provenance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFromSourceStatus(t *testing.T) {
	now := time.Date(2026, 11, 3, 14, 32, 0, 0, time.UTC)
	cases := []struct {
		provider, url, doc, want string
	}{
		{"Reuters", "https://example.com/a", "", Known},
		{"SEC", "", "0000320193-26-000001", Known},
		{"Reuters", "", "", Partial},
		{"", "", "", Unknown},
	}
	for _, c := range cases {
		f := FromSource(c.provider, c.url, c.doc, time.Time{}, now)
		if f.Status != c.want || f.Validate() != nil {
			t.Errorf("%+v: status %q, err %v", c, f.Status, f.Validate())
		}
	}
}

func TestValidateRefusesKnownWithoutOrigin(t *testing.T) {
	if (Fact{Kind: Observed, Status: Known}).Validate() == nil {
		t.Error("a known observed fact with no origin must be refused")
	}
	if (Fact{Kind: Calculated, Status: Known}).Validate() == nil {
		t.Error("a calculated fact with no method must be refused")
	}
	if (Fact{Kind: "guess", Status: Known}).Validate() == nil {
		t.Error("an unknown kind must be refused")
	}
	if (Fact{Kind: Observed, Status: "maybe"}).Validate() == nil {
		t.Error("an unknown status must be refused")
	}
	if Unspecified("no source recorded").Validate() != nil {
		t.Error("unknown must stay expressible")
	}
}

func TestFactJSONIsStableAndUTC(t *testing.T) {
	zone := time.FixedZone("CET", 3600)
	f := FromSource("Reuters", "https://example.com/a", "", time.Date(2026, 11, 3, 15, 32, 0, 0, zone), time.Time{})
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{`"kind":"observed"`, `"status":"known"`, `"published_at":"2026-11-03T14:32:00Z"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s missing from %s", want, got)
		}
	}
	if strings.Contains(got, "official") || strings.Contains(got, "retrieved_at") {
		t.Errorf("unset fields must be omitted: %s", got)
	}
}

func TestCalculationNeedsMethod(t *testing.T) {
	if _, err := Calculation(Method{}); err == nil {
		t.Error("a method id is required")
	}
	f, err := Calculation(Method{ID: "sma", Version: "1", Formula: "mean(close, 20)"}, "quote:AAPL")
	if err != nil || f.Status != Known || len(f.Inputs) != 1 {
		t.Errorf("fact=%+v err=%v", f, err)
	}
}
