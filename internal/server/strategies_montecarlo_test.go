package server

import (
	"net/http"
	"strings"
	"testing"
)

// frequent enters whenever flat and leaves after three bars, so a short series gives many trades.
const frequent = `{"name":"Dip buyer","direction":"long","entry":[{"left":{"type":"price","field":"close"},"op":">","right":{"type":"const","value":0}}],"max_bars":3}`

func TestStrategyMonteCarloIsReproducibleAndKeepsObservedApart(t *testing.T) {
	e := strategyEngine()
	body := `{"definition":` + frequent + `,"symbol":"TEST","range":"5Y","method":"trade_bootstrap","runs":200,"seed":11,"drawdown_limit":-10}`
	code, a := callStrategies(e, http.MethodPost, "/api/strategies/montecarlo", body)
	if code != http.StatusOK {
		t.Fatalf("status %d %v", code, a)
	}
	_, b := callStrategies(e, http.MethodPost, "/api/strategies/montecarlo", body)
	sa, sb := a["simulation"].(map[string]any), b["simulation"].(map[string]any)
	if sa["terminal_return_pct"].(map[string]any)["p50"] != sb["terminal_return_pct"].(map[string]any)["p50"] {
		t.Fatal("same seed must reproduce the result")
	}
	if a["observed"] == nil || sa["model_version"] == nil || a["data_start"] == nil || a["strategy"] != "Dip buyer" {
		t.Fatalf("provenance or observed figures missing: %v", a)
	}
	if cfg := sa["config"].(map[string]any); cfg["seed"] != float64(11) || cfg["runs"] != float64(200) {
		t.Fatalf("settings not echoed: %v", cfg)
	}
}

func TestStrategyMonteCarloRefusesBadInput(t *testing.T) {
	e := strategyEngine()
	for name, body := range map[string]string{
		"bad method": `{"definition":` + myStrategy + `,"symbol":"TEST","method":"block"}`,
		"bad runs":   `{"definition":` + myStrategy + `,"symbol":"TEST","runs":5}`,
		"unknown":    `{"definition":` + myStrategy + `,"symbol":"TEST","nope":1}`,
	} {
		code, res := callStrategies(e, http.MethodPost, "/api/strategies/montecarlo", body)
		if code < 400 || res["error"] == nil {
			t.Fatalf("%s: %d %v", name, code, res)
		}
	}
}

func TestStrategyMonteCarloTooFewTradesIsRefusedNotInvented(t *testing.T) {
	e := strategyEngine()
	// A rule that never enters has no trades to resample.
	never := `{"name":"Never","direction":"long","entry":[{"left":{"type":"rsi","period":14},"op":"<","right":{"type":"const","value":-5}}],"max_bars":3}`
	code, res := callStrategies(e, http.MethodPost, "/api/strategies/montecarlo", `{"definition":`+never+`,"symbol":"TEST"}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(res["error"].(string), "only 0 closed trades") {
		t.Fatalf("expected a refusal, got %d %v", code, res)
	}
}

func TestStrategyMonteCarloSensitivitiesReproduceAndAreEchoed(t *testing.T) {
	e := strategyEngine()
	body := `{"definition":` + frequent + `,"symbol":"TEST","range":"5Y","runs":200,"seed":4,"delay_bars":2,"regime_share_pct":25,"regime_scale":2,"position_scale":1.5}`
	code, a := callStrategies(e, http.MethodPost, "/api/strategies/montecarlo", body)
	if code != http.StatusOK {
		t.Fatalf("status %d %v", code, a)
	}
	_, b := callStrategies(e, http.MethodPost, "/api/strategies/montecarlo", body)
	sa, sb := a["simulation"].(map[string]any), b["simulation"].(map[string]any)
	if sa["terminal_return_pct"].(map[string]any)["p50"] != sb["terminal_return_pct"].(map[string]any)["p50"] {
		t.Fatal("same seed must reproduce the result")
	}
	cfg := sa["config"].(map[string]any)
	if cfg["delay_bars"] != float64(2) || cfg["regime_share_pct"] != float64(25) || cfg["position_scale"] != 1.5 || cfg["instrument_daily_vol_pct"] == nil {
		t.Fatalf("settings not echoed: %v", cfg)
	}
	if sa["concentration"] == nil {
		t.Fatal("concentration missing")
	}
}

func TestStrategyMonteCarloRefusesBadSensitivities(t *testing.T) {
	e := strategyEngine()
	for name, extra := range map[string]string{"delay": `"delay_bars":9`, "position": `"position_scale":9`, "share": `"regime_share_pct":150`} {
		code, res := callStrategies(e, http.MethodPost, "/api/strategies/montecarlo", `{"definition":`+frequent+`,"symbol":"TEST","range":"5Y",`+extra+`}`)
		if code < 400 || res["error"] == nil {
			t.Fatalf("%s: %d %v", name, code, res)
		}
	}
}
