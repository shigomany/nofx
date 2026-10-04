package trader

import (
	"nofx/kernel"
	"nofx/store"
	"testing"
	"time"
)

func nativeRuleTrader() *AutoTrader {
	cfg := store.GetRuleBasedStrategyConfig(store.RuleBasedPresetTrendFollowing)
	return &AutoTrader{exchange: "hyperliquid", strategyEngine: kernel.NewStrategyEngine(&cfg)}
}

func nativeRuleDecision(now time.Time) kernel.Decision {
	return kernel.Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 2,
		PositionSizeUSD: 22.5, StopLoss: 98.5, TakeProfit: 104.5,
		RuleATR14: 1, RuleSignalTimeMS: now.Truncate(4 * time.Hour).UnixMilli()}
}

func TestNativeRuleExecutionRefreshesRisk(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	at := nativeRuleTrader()
	d := nativeRuleDecision(now)
	if err := at.prepareRuleBasedOpen(&d, 30, 30, 0, 102, now); err != nil {
		t.Fatal(err)
	}
	if d.PositionSizeUSD > 15 || d.RiskUSD > .30 || d.StopLoss != 100.5 || d.TakeProfit != 106.5 {
		t.Fatalf("fresh limits not applied: %+v", d)
	}
	for _, change := range []func(*kernel.Decision){
		func(d *kernel.Decision) { d.RuleSignalTimeMS -= int64(4 * time.Hour / time.Millisecond) },
		func(d *kernel.Decision) { d.Leverage = 10 },
		func(d *kernel.Decision) { d.Action = "open_short" },
		func(d *kernel.Decision) { d.RuleATR14 = 4 },
	} {
		d = nativeRuleDecision(now)
		change(&d)
		if err := at.prepareRuleBasedOpen(&d, 45, 45, 0, 100, now); err == nil {
			t.Fatalf("unsafe order accepted: %+v", d)
		}
	}
	d = nativeRuleDecision(now)
	if err := at.prepareRuleBasedOpen(&d, 45, 45, 12, 100, now); err == nil {
		t.Fatal("reserved margin ignored")
	}
}

func TestNativeRuleHistorySurvivesRestart(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	st, err := store.New(t.TempDir() + "/orders.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	at := nativeRuleTrader()
	at.id, at.store = "rules", st
	d := nativeRuleDecision(now)
	if got := at.ruleBasedThrottleReason(d, now); got != "" {
		t.Fatalf("new signal blocked: %s", got)
	}
	if err := st.Order().CreateOrder(&store.TraderOrder{TraderID: at.id, ExchangeID: "test", ExchangeOrderID: "order1", Symbol: d.Symbol, OrderAction: "open_long", Side: "BUY", Type: "MARKET", Quantity: .1, Status: "FILLED", CreatedAt: now.Add(-time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	// A new runtime object uses the same audit database, no in-memory cache.
	restarted := nativeRuleTrader()
	restarted.id, restarted.store = at.id, st
	if got := restarted.ruleBasedThrottleReason(d, now); got == "" {
		t.Fatal("same signal reused after restart")
	}
	if got := restarted.ruleBasedThrottleReason(nativeRuleDecision(now.Add(4*time.Hour)), now.Add(4*time.Hour)); got != "" {
		t.Fatalf("next candle blocked: %s", got)
	}
}

func TestNativeRuleTrendExitBypassesAINoiseThrottle(t *testing.T) {
	at := nativeRuleTrader()
	ctx := throttleContext("BTCUSDT", "long", time.Minute, 0)
	if got := at.tradeThrottleReason(kernel.Decision{Symbol: "BTCUSDT", Action: "close_long"}, ctx); got != "" {
		t.Fatalf("rule exit delayed: %s", got)
	}
	if got := at.ruleBasedThrottleReason(nativeRuleDecision(time.Now()), time.Now()); got == "" {
		t.Fatal("entry without persistent history accepted")
	}
}
