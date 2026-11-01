package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"floweysales/internal/trading"
)

func sizeCall(e *Engine, body string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trading/size", strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestPositionSizingFromATRAndFromAStop(t *testing.T) {
	e := backtestEngine(t)
	e.paperState = func() ([]trading.Position, trading.Summary) {
		return nil, trading.Summary{Equity: 100000, BuyingPower: 100000}
	}
	code, body := sizeCall(e, `{"symbol":"UP","side":"buy","risk_pct":1,"max_weight_pct":100}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	res := body["result"].(map[string]any)
	if body["stop_source"] != "atr" || res["quantity"].(float64) < 1 || res["stop"].(float64) >= body["entry"].(float64) {
		t.Fatalf("an ATR stop sits below a long entry: %v", body)
	}
	if risk := res["risk_amount"].(float64); risk > 1000 || risk < 900 {
		t.Fatalf("risk %v should be close to 1 %% of equity", risk)
	}

	code, body = sizeCall(e, `{"symbol":"UP","side":"buy","entry":100,"stop":95,"max_weight_pct":100}`)
	if code != http.StatusOK || body["stop_source"] != "given" || body["result"].(map[string]any)["quantity"].(float64) != 200 {
		t.Fatalf("%d %v", code, body)
	}
}

func TestPositionSizingRejectsBadRequests(t *testing.T) {
	e := backtestEngine(t)
	e.paperState = func() ([]trading.Position, trading.Summary) {
		return nil, trading.Summary{Equity: 100000, BuyingPower: 100000}
	}
	for name, body := range map[string]string{
		"not json": `x`, "unknown field": `{"symbol":"UP","side":"buy","qty":3}`, "no symbol": `{"side":"buy"}`,
		"bad side": `{"symbol":"UP","side":"hold","entry":10,"stop":9}`, "stop wrong side": `{"symbol":"UP","side":"buy","entry":10,"stop":11}`,
		"risk too big": `{"symbol":"UP","side":"buy","entry":10,"stop":9,"risk_pct":9}`,
	} {
		if code, _ := sizeCall(e, body); code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, code)
		}
	}
	e.providers = investorBackend{now: time.Now().UTC()}
	if code, _ := sizeCall(e, `{"symbol":"BAD","side":"buy"}`); code == http.StatusOK {
		t.Error("a failing symbol cannot be sized")
	}
}
