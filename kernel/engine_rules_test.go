package kernel

import (
	"math"
	"nofx/market"
	"nofx/store"
	"testing"
	"time"
)

func ruleFixture(now time.Time, preset string) *Context {
	frames := make(map[string]*market.TimeframeSeriesData)
	for tf, step := range map[string]time.Duration{"4h": 4 * time.Hour, "1d": 24 * time.Hour} {
		s := &market.TimeframeSeriesData{Timeframe: tf}
		start := now.Truncate(step).Add(-29 * step)
		for i := 0; i < 30; i++ {
			s.Klines = append(s.Klines, market.KlineBar{Time: start.Add(time.Duration(i) * step).UnixMilli(), Open: 100, High: 100.5, Low: 99.5, Close: 100})
			s.EMA20Values = append(s.EMA20Values, 99)
			s.EMA50Values = append(s.EMA50Values, 95)
			s.RSI14Values = append(s.RSI14Values, 50)
		}
		frames[tf] = s
	}
	s := frames["4h"]
	price := 101.0
	s.Klines[28] = market.KlineBar{Time: s.Klines[28].Time, Open: 100, Close: 101, High: 101.5, Low: 99.5}
	if preset == "rsi_pullback" {
		price = 98
		s.Klines[28] = market.KlineBar{Time: s.Klines[28].Time, Open: 98, Close: 98, High: 98.5, Low: 97.5}
		s.RSI14Values[27], s.RSI14Values[28] = 29, 31
	}
	return &Context{Exchange: "hyperliquid", Account: AccountInfo{TotalEquity: 45, AvailableBalance: 45},
		MarketDataMap: map[string]*market.Data{"BTCUSDT": {Symbol: "BTCUSDT", CurrentPrice: price, TimeframeData: frames}}}
}

func findRuleOpen(out *FullDecision) *Decision {
	for i := range out.Decisions {
		if out.Decisions[i].Action == "open_long" {
			return &out.Decisions[i]
		}
	}
	return nil
}

func TestRuleBasedEntriesAndNoAI(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	for _, preset := range []string{"trend_following", "rsi_pullback"} {
		t.Run(preset, func(t *testing.T) {
			ctx := ruleFixture(now, preset)
			cfg := store.GetRuleBasedStrategyConfig(preset)
			// A nil AI client proves this branch cannot call a model.
			out, err := GetFullDecisionWithStrategy(ctx, nil, NewStrategyEngine(&cfg), "")
			if err != nil || out == nil || out.AIRequestDurationMs != 0 {
				t.Fatalf("native evaluation: %v, %#v", err, out)
			}
			// Use fixture time separately from the real-time entry point.
			out, err = EvaluateRuleBased(ctx, preset, now)
			d := findRuleOpen(out)
			if err != nil || d == nil {
				t.Fatalf("expected closed-bar entry: %v %#v", err, out)
			}
			if d.PositionSizeUSD > 22.5 || d.RiskUSD > 0.45 || d.PositionSizeUSD < 12 || d.Leverage != 2 || d.StopLoss <= 0 || d.TakeProfit <= ctx.MarketDataMap["BTCUSDT"].CurrentPrice {
				t.Fatalf("unsafe size: %#v", d)
			}
			if d.RuleSignalTimeMS != now.Truncate(4*time.Hour).UnixMilli() {
				t.Fatal("wrong signal candle")
			}
		})
	}
}

func TestRuleBasedIgnoresFormingSignal(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	ctx := ruleFixture(now, "trend_following")
	s := ctx.MarketDataMap["BTCUSDT"].TimeframeData["4h"]
	s.Klines[28].Close, s.Klines[28].High = 100, 100.5
	s.Klines[29].Close, s.Klines[29].High = 500, 501
	s.EMA20Values[29] = 400
	out, _ := EvaluateRuleBased(ctx, "trend_following", now)
	if findRuleOpen(out) != nil {
		t.Fatal("forming candle triggered entry")
	}
}

func TestRuleBasedFiltersAndFailClosed(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	cases := map[string]func(*Context){
		"daily downtrend":      func(c *Context) { c.MarketDataMap["BTCUSDT"].TimeframeData["1d"].EMA20Values[28] = 90 },
		"existing exposure":    func(c *Context) { c.Positions = []PositionInfo{{Symbol: "SOLUSDT", Side: "long"}} },
		"reported exposure":    func(c *Context) { c.Account.PositionCount = 1 },
		"small account":        func(c *Context) { c.Account.TotalEquity = 20 },
		"invalid equity":       func(c *Context) { c.Account.TotalEquity = math.NaN() },
		"unaligned indicators": func(c *Context) { c.MarketDataMap["BTCUSDT"].TimeframeData["4h"].EMA20Values = nil },
		"future bars": func(c *Context) {
			c.MarketDataMap["BTCUSDT"].TimeframeData["4h"].Klines[29].Time += int64(4 * time.Hour / time.Millisecond)
		},
		"missing daily": func(c *Context) { delete(c.MarketDataMap["BTCUSDT"].TimeframeData, "1d") },
		"stale": func(c *Context) {
			for i := range c.MarketDataMap["BTCUSDT"].TimeframeData["4h"].Klines {
				c.MarketDataMap["BTCUSDT"].TimeframeData["4h"].Klines[i].Time -= int64(8 * time.Hour / time.Millisecond)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := ruleFixture(now, "trend_following")
			change(ctx)
			out, err := EvaluateRuleBased(ctx, "trend_following", now)
			if err != nil || findRuleOpen(out) != nil {
				t.Fatalf("unsafe entry: %v %#v", err, out)
			}
		})
	}
	ctx := ruleFixture(now, "trend_following")
	if _, err := EvaluateRuleBased(ctx, "unknown", now); err == nil {
		t.Fatal("unknown preset accepted")
	}
	ctx.Exchange = "bybit"
	if _, err := EvaluateRuleBased(ctx, "trend_following", now); err == nil {
		t.Fatal("wrong exchange accepted")
	}
}

func TestRuleBasedSizingLimits(t *testing.T) {
	for _, tc := range []struct {
		equity, available, used, price, atr float64
		rejected                            bool
	}{
		{45, 45, 0, 100, .1, false}, {45, 45, 0, 100, 2, false}, {45, 45, 0, 100, 3, true},
		{20, 20, 0, 100, .1, true}, {45, 2, 0, 100, .1, true}, {45, 45, 14, 100, .1, true},
		{45, 45, 0, math.NaN(), 1, true},
	} {
		n, stop, tp, loss, err := SizeRuleBasedLong(tc.equity, tc.available, tc.used, tc.price, tc.atr)
		if (err != nil) != tc.rejected {
			t.Fatalf("unexpected sizing: %+v: %v", tc, err)
		}
		if err == nil && (n > tc.equity*.5 || loss > tc.equity*.01+1e-9 || n/2+tc.used > tc.equity*.3 || (tp-tc.price)/(tc.price-stop) < 2.999999) {
			t.Fatalf("risk breached: n=%v loss=%v", n, loss)
		}
	}
}

func TestRuleBasedExitOnTrendInvalidation(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	ctx := ruleFixture(now, "trend_following")
	ctx.Positions = []PositionInfo{{Symbol: "BTCUSDT", Side: "long"}}
	ctx.MarketDataMap["BTCUSDT"].TimeframeData["1d"].EMA20Values[28] = 90
	out, _ := EvaluateRuleBased(ctx, "trend_following", now)
	if len(out.Decisions) != 1 || out.Decisions[0].Action != "close_long" {
		t.Fatalf("expected exit only: %#v", out.Decisions)
	}
}
