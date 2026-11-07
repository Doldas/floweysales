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

func TestSensitivitiesAreSeededAndLabeled(t *testing.T) {
	cfg := TradeConfig{Runs: 400, Seed: 5, DelayBars: 2, InstrumentDailyVolPct: 1.5, Exposure: 1, RegimeSharePct: 30, RegimeScale: 3, PositionScale: 1.5}
	a, err := RunTrades(tradeSeries(40), cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := RunTrades(tradeSeries(40), cfg)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("sensitivities must be reproducible with the same seed")
	}
	cfg.Seed = 6
	c, _ := RunTrades(tradeSeries(40), cfg)
	if reflect.DeepEqual(a.TerminalReturnPct, c.TerminalReturnPct) {
		t.Fatal("a different seed should change the percentiles")
	}
	text := strings.Join(a.Assumptions, " ")
	for _, want := range []string{"fill delay of 2 bar", "stretch of 30%", "multiplied by 1.5"} {
		if !strings.Contains(text, want) {
			t.Fatalf("assumption %q missing: %s", want, text)
		}
	}
}

func TestSensitivitiesWidenTheBand(t *testing.T) {
	width := func(c TradeConfig) float64 {
		c.Runs, c.Seed = 500, 8
		r, err := RunTrades(tradeSeries(40), c)
		if err != nil {
			t.Fatal(err)
		}
		return r.TerminalReturnPct.P95 - r.TerminalReturnPct.P5
	}
	base := width(TradeConfig{})
	for name, c := range map[string]TradeConfig{
		"delay":    {DelayBars: 3, InstrumentDailyVolPct: 2, Exposure: 1},
		"regime":   {RegimeSharePct: 40, RegimeScale: 3},
		"position": {PositionScale: 2},
	} {
		if width(c) <= base {
			t.Fatalf("%s must widen the band", name)
		}
	}
}

func TestSensitivitiesRejectBadSettings(t *testing.T) {
	for name, bad := range map[string]TradeConfig{
		"delay too long":        {DelayBars: 9, InstrumentDailyVolPct: 1, Exposure: 1},
		"delay without inputs":  {DelayBars: 1},
		"share over 100":        {RegimeSharePct: 120, RegimeScale: 2},
		"regime scale too high": {RegimeSharePct: 10, RegimeScale: 9},
		"position too big":      {PositionScale: 5},
		"position too small":    {PositionScale: 0.1},
	} {
		if _, err := RunTrades(tradeSeries(40), bad); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
}

func TestConcentrationUsesObservedTradesOnly(t *testing.T) {
	res, err := RunTrades(tradeSeries(40), TradeConfig{Runs: 200, Seed: 1, PositionScale: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Concentration.BestTrades != BestTradesN || res.Concentration.LargestLossPct != -2 {
		t.Fatalf("concentration wrong: %+v", res.Concentration)
	}
	if res.Concentration.ReturnWithoutBestPct >= res.Observed.TotalReturnPct {
		t.Fatalf("removing the best trades must lower the return: %+v vs %v", res.Concentration, res.Observed.TotalReturnPct)
	}
}

func TestNoTradesIsRefusedWithCount(t *testing.T) {
	_, err := RunTrades([]float64{}, TradeConfig{})
	if err == nil || !strings.Contains(err.Error(), "only 0 closed trades") {
		t.Fatalf("expected a count in the refusal, got %v", err)
	}
}
