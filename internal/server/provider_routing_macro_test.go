package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"floweysales/internal/market/markettest"
)

var macroNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

// macroEngine installs the providers; each listed in serving declares macro
// and has every series the regime reads.
func macroEngine(t *testing.T, providers []string, serving ...string) (*Engine, *markettest.Backend) {
	t.Helper()
	e, b := routingEngine(t, providers...)
	for _, p := range serving {
		b.Declare(p, "macro")
		serveMacroSeries(b, p, "")
	}
	return e, b
}

// serveMacroSeries gives a provider every series, or only the one named.
func serveMacroSeries(b *markettest.Backend, provider, only string) {
	for id, s := range calmSeries(macroNow) {
		if only == "" || only == id {
			b.Serve(provider, id, s)
		}
	}
}

func macroRow(t *testing.T, out string) map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("%v %s", err, out)
	}
	return rows[0]
}

func TestMacroWithoutPolicyUsesTheProviderThatDeclaresIt(t *testing.T) {
	e, _ := macroEngine(t, []string{"AAA", "MMM"}, "MMM")
	out, err := e.runMacroNode(context.Background(), macroNow)
	if err != nil {
		t.Fatal(err)
	}
	row := macroRow(t, out)
	if row["provider"] != "MMM" || row["regime"] == nil || row["routing"] != nil {
		t.Errorf("%v", row)
	}
}

func TestMacroStillFindsAFREDPluginThatPredatesTheCapability(t *testing.T) {
	e, b := routingEngine(t, "AAA", "FRED")
	serveMacroSeries(b, "FRED", "")
	out, err := e.runMacroNode(context.Background(), macroNow)
	if err != nil {
		t.Fatal(err)
	}
	if row := macroRow(t, out); row["provider"] != "FRED" {
		t.Errorf("%v", row)
	}
	_, view, raw := icall(e, "GET", "/api/providers/routing", nil)
	for _, d := range view["domains"].([]any) {
		if dm := d.(map[string]any); dm["id"] == "macro" && (dm["routed"] != true || int(dm["sources"].(float64)) != 1) {
			t.Errorf("the macro domain must list FRED only: %s", raw)
		}
	}
}

func TestMacroWithNoProviderGivesANeutralHint(t *testing.T) {
	e, _ := routingEngine(t, "AAA")
	_, err := e.runMacroNode(context.Background(), macroNow)
	if err == nil || !strings.Contains(err.Error(), "Install a provider plugin that offers macro series") {
		t.Errorf("%v", err)
	}
}

func TestMacroPolicyRefusesAProviderThatDoesNotDeclareIt(t *testing.T) {
	e, _ := macroEngine(t, []string{"AAA", "MMM"}, "MMM")
	if code, out := putPolicy(e, "macro", map[string]any{"primary": "AAA"}); code != http.StatusUnprocessableEntity {
		t.Errorf("%d %v", code, out)
	}
}

func TestMacroFollowsThePolicyPrimary(t *testing.T) {
	e, _ := macroEngine(t, []string{"AAA", "BBB"}, "AAA", "BBB")
	if code, out := putPolicy(e, "macro", map[string]any{"primary": "BBB"}); code != 200 {
		t.Fatalf("%v", out)
	}
	out, err := e.runMacroNode(context.Background(), macroNow)
	if err != nil {
		t.Fatal(err)
	}
	row := macroRow(t, out)
	routing, _ := row["routing"].(map[string]any)
	if row["provider"] != "BBB" || routing == nil || routing["actual"] != "BBB" || routing["fell_back"] != false {
		t.Errorf("%v", row)
	}
}

func TestMacroFallsBackForTheWholeRegimeAndSaysSo(t *testing.T) {
	e, b := macroEngine(t, []string{"AAA", "BBB"}, "BBB")
	b.Declare("AAA", "macro") // declares it, but answers no series
	putPolicy(e, "macro", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}})
	out, err := e.runMacroNode(context.Background(), macroNow)
	if err != nil {
		t.Fatal(err)
	}
	row := macroRow(t, out)
	routing, _ := row["routing"].(map[string]any)
	if row["provider"] != "BBB" || routing == nil || routing["requested"] != "AAA" || routing["fell_back"] != true {
		t.Fatalf("%v", row)
	}
	if limits, _ := routing["limitations"].([]any); len(limits) != 1 {
		t.Errorf("a fallback must be disclosed: %v", routing)
	}
}

func TestMacroNeverMixesSeriesFromTwoProviders(t *testing.T) {
	e, b := macroEngine(t, []string{"AAA", "BBB"}, "BBB")
	b.Declare("AAA", "macro")
	serveMacroSeries(b, "AAA", "VIXCLS")
	putPolicy(e, "macro", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}})
	out, err := e.runMacroNode(context.Background(), macroNow)
	if err != nil {
		t.Fatal(err)
	}
	row := macroRow(t, out)
	if row["provider"] != "AAA" || !strings.Contains(row["missing"].(string), "UNRATE") || row["unemployment"] != nil {
		t.Errorf("a partial answer stays partial and names what is missing: %v", row)
	}
	if routing, _ := row["routing"].(map[string]any); routing == nil || routing["fell_back"] != false {
		t.Errorf("%v", row)
	}
}

func TestMacroPolicyWithoutFallbackFailsInsteadOfSubstituting(t *testing.T) {
	e, b := macroEngine(t, []string{"AAA", "BBB"}, "AAA", "BBB")
	putPolicy(e, "macro", map[string]any{"primary": "AAA"})
	var faults []markettest.Fault
	for range macroSeries {
		faults = append(faults, markettest.Fault{Err: errors.New("HTTP 503")})
	}
	b.Inject(markettest.OpHistory, faults...)
	_, err := e.runMacroNode(context.Background(), macroNow)
	if err == nil || !strings.Contains(err.Error(), "AAA") || !strings.Contains(err.Error(), "503") {
		t.Errorf("%v", err)
	}
	if got := b.Calls(markettest.OpHistory); got != len(macroSeries) {
		t.Errorf("no other provider is asked: %d calls", got)
	}
}

func TestMacroPolicyWhoseSourceIsGoneFailsClosed(t *testing.T) {
	e, _ := macroEngine(t, []string{"AAA", "BBB"}, "AAA", "BBB")
	putPolicy(e, "macro", map[string]any{"primary": "AAA"})
	remaining := markettest.New("BBB")
	remaining.Declare("BBB", "macro")
	serveMacroSeries(remaining, "BBB", "")
	e.providers = remaining
	if out, err := e.runMacroNode(context.Background(), macroNow); err == nil {
		t.Errorf("another provider must not answer silently: %s", out)
	}
}
