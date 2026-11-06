package montecarlo

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func tradeSeries(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		switch i % 4 {
		case 0:
			out[i] = 3
		case 1:
			out[i] = -1.5
		case 2:
			out[i] = 0.5
		default:
			out[i] = -2
		}
	}
	return out
}

func TestTradesSameSeedSameResult(t *testing.T) {
	cfg := TradeConfig{Method: TradeBootstrap, Runs: 300, Seed: 9}
	a, err := RunTrades(tradeSeries(40), cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := RunTrades(tradeSeries(40), cfg)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same inputs and seed must give the same result")
	}
	cfg.Seed = 10
	c, _ := RunTrades(tradeSeries(40), cfg)
	if reflect.DeepEqual(a.TerminalReturnPct, c.TerminalReturnPct) {
		t.Fatal("a different seed should change the percentiles")
	}
	if a.ModelVersion != ModelVersion || a.Config.Seed != 9 || a.Config.Trades != 40 {
		t.Fatalf("settings not reported: %+v", a.Config)
	}
}

func TestTradeShuffleKeepsTotalReturn(t *testing.T) {
	res, err := RunTrades(tradeSeries(40), TradeConfig{Method: TradeShuffle, Runs: 200, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(res.TerminalReturnPct.P5-res.TerminalReturnPct.P95) > 0.02 || math.Abs(res.TerminalReturnPct.P50-res.Observed.TotalReturnPct) > 0.02 {
		t.Fatalf("shuffle must keep the total: %+v vs %v", res.TerminalReturnPct, res.Observed.TotalReturnPct)
	}
	if _, err := RunTrades(tradeSeries(40), TradeConfig{Method: TradeShuffle, Trades: 30}); err == nil {
		t.Fatal("shuffle with another length must be refused")
	}
}

func TestTradeBootstrapOrderedAndNonPositiveDrawdown(t *testing.T) {
	res, err := RunTrades(tradeSeries(40), TradeConfig{Runs: 500, Seed: 3, DrawdownLimitPct: -5})
	if err != nil {
		t.Fatal(err)
	}
	d := res.TerminalReturnPct
	if !(d.P5 <= d.P25 && d.P25 <= d.P50 && d.P50 <= d.P75 && d.P75 <= d.P95) {
		t.Fatalf("not ordered: %+v", d)
	}
	if res.MaxDrawdownPct.P95 > 0 || res.ProbBreachPct == nil {
		t.Fatalf("drawdown or breach wrong: %+v", res)
	}
	if res.ProbLossPct < 0 || res.ProbLossPct > 100 {
		t.Fatal("probability out of range")
	}
}

func TestTradesRejectSparseFlatAndBroken(t *testing.T) {
	for name, in := range map[string][]float64{
		"none":   nil,
		"sparse": tradeSeries(MinTrades - 1),
		"flat":   make([]float64, 40),
		"nan":    append(tradeSeries(30), math.NaN()),
		"under":  append(tradeSeries(30), -120),
	} {
		if _, err := RunTrades(in, TradeConfig{}); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
	for _, bad := range []TradeConfig{{Method: "x"}, {Runs: 5}, {Trades: 3}, {DrawdownLimitPct: 5}, {ExtraCostBps: -1}, {DispersionScale: 9}} {
		if _, err := RunTrades(tradeSeries(40), bad); err == nil {
			t.Fatalf("%+v must be refused", bad)
		}
	}
}

func TestTradesExtremeLossIsRuin(t *testing.T) {
	in := append(tradeSeries(30), -100, -100)
	res, err := RunTrades(in, TradeConfig{Runs: 200, Seed: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProbRuinPct <= 0 || res.TerminalReturnPct.P5 != -100 || res.MaxDrawdownPct.P5 != -100 {
		t.Fatalf("a total loss must show as ruin: %+v", res)
	}
}

func TestTradeStressesLowerAndWiden(t *testing.T) {
	base, _ := RunTrades(tradeSeries(40), TradeConfig{Runs: 400, Seed: 5})
	cost, _ := RunTrades(tradeSeries(40), TradeConfig{Runs: 400, Seed: 5, ExtraCostBps: 100})
	wide, _ := RunTrades(tradeSeries(40), TradeConfig{Runs: 400, Seed: 5, DispersionScale: 2})
	if cost.TerminalReturnPct.P50 >= base.TerminalReturnPct.P50 {
		t.Fatal("extra cost must lower the median")
	}
	if wide.TerminalReturnPct.P95-wide.TerminalReturnPct.P5 <= base.TerminalReturnPct.P95-base.TerminalReturnPct.P5 {
		t.Fatal("larger dispersion must widen the band")
	}
	if !strings.Contains(strings.Join(cost.Assumptions, " "), "stress you chose") {
		t.Fatal("stresses must be labeled as chosen")
	}
}

func TestTradeLimitationsDiscloseSmallSamples(t *testing.T) {
	res, _ := RunTrades(tradeSeries(24), TradeConfig{Runs: 200, Seed: 1})
	if !strings.Contains(strings.Join(res.Limitations, " "), "Only 24 trades") {
		t.Fatal("small sample must be disclosed")
	}
	if !strings.Contains(strings.Join(res.Assumptions, " "), "closed trades") {
		t.Fatal("drawdown basis must be disclosed")
	}
}
