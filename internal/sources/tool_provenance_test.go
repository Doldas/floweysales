package sources

import (
	"encoding/json"
	"testing"
	"time"

	"floweysales/internal/provenance"
)

var toolNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func factOf(t *testing.T, out string) provenance.Fact {
	t.Helper()
	var doc struct {
		Provenance *provenance.Fact `json:"provenance"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.Provenance == nil {
		t.Fatalf("no provenance in %s (%v)", out, err)
	}
	if err := doc.Provenance.Validate(); err != nil {
		t.Fatalf("invalid fact: %v", err)
	}
	return *doc.Provenance
}

func TestEveryDataToolResultGetsAFactOrIsExempt(t *testing.T) {
	const in = `{"provider":"YAHOO","value":1}`
	for tool, o := range ToolOrigins {
		out := attachToolProvenance(tool, in, toolNow)
		if o.Kind == OriginRegistered || o.Kind == OriginExempt {
			if out != in {
				t.Errorf("%s should pass through: %s", tool, out)
			}
			continue
		}
		f := factOf(t, out)
		if f.Kind != o.Kind || f.RetrievedAt == nil {
			t.Errorf("%s: %+v", tool, f)
		}
		if o.Kind == provenance.Calculated && (f.Method == nil || f.Method.ID == "") {
			t.Errorf("%s: calculated without a method", tool)
		}
		if f.Inputs[len(f.Inputs)-1] != "tool:"+tool {
			t.Errorf("%s: the tool is not named as an input: %v", tool, f.Inputs)
		}
	}
}

func TestToolFactNamesTheResultsProviderElseSaysUnknown(t *testing.T) {
	named := factOf(t, attachToolProvenance("get_price_history", `{"provider":"YAHOO","points":[]}`, toolNow))
	if named.Provider != "YAHOO" || named.Status != provenance.Partial {
		t.Fatalf("named = %+v", named)
	}
	edgar := factOf(t, attachToolProvenance("get_sec_filings", `{"filings":[]}`, toolNow))
	if edgar.Provider != "SEC EDGAR" {
		t.Fatalf("sec = %+v", edgar)
	}
	unknown := factOf(t, attachToolProvenance("get_latest_close", `{"close":1}`, toolNow))
	if unknown.Status != provenance.Unknown || unknown.Note == "" {
		t.Fatalf("a result that names no source must be unknown: %+v", unknown)
	}
}

func TestToolProvenanceKeepsExistingFactsAndErrors(t *testing.T) {
	has := `{"provenance":{"kind":"calculated","status":"known"},"rows":[]}`
	if out := attachToolProvenance("get_leaderboard", has, toolNow); out != has {
		t.Fatalf("existing fact replaced: %s", out)
	}
	for _, in := range []string{`{"error":"no key"}`, `not json`, `[1]`, ``} {
		if out := attachToolProvenance("get_price_history", in, toolNow); out != in {
			t.Errorf("%q changed to %q", in, out)
		}
	}
}

func TestRegisterAddsToolProvenanceButNoSources(t *testing.T) {
	r := NewRegistry()
	out, added := r.Register("calculate_dcf", "c1", "{}", `{"per_share":10}`)
	if len(added) != 0 {
		t.Fatal("a calculation is not a citable source")
	}
	if f := factOf(t, out); f.Kind != provenance.Calculated {
		t.Fatalf("fact = %+v", f)
	}
}
