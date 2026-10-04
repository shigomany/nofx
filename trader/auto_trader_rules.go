package trader

import (
	"fmt"
	"math"
	"nofx/kernel"
	"nofx/market"
	"nofx/store"
	"time"
)

func (at *AutoTrader) usesRuleBasedStrategy() bool {
	return at.strategyEngine != nil && at.strategyEngine.GetConfig().RuleBased != nil
}

func (at *AutoTrader) prepareRuleBasedOpen(d *kernel.Decision, equity, available, marginUsed, price float64, now time.Time) error {
	cfg := at.strategyEngine.GetConfig()
	if at.exchange != "hyperliquid" || at.config.HyperliquidTestnet || !store.IsSupportedRuleBasedPreset(cfg.RuleBased.Preset) ||
		d.Action != "open_long" || (d.Symbol != "BTCUSDT" && d.Symbol != "ETHUSDT") || d.Leverage != kernel.RuleLeverage {
		return fmt.Errorf("invalid native indicator order")
	}
	if d.RuleSignalTimeMS != now.Truncate(4*time.Hour).UnixMilli() {
		return fmt.Errorf("indicator signal candle expired")
	}
	if math.IsNaN(d.PositionSizeUSD) || math.IsInf(d.PositionSizeUSD, 0) || d.PositionSizeUSD < 12 {
		return fmt.Errorf("invalid indicator position size")
	}
	// Equity, available funds, reserved margin, native mark, and all sizing
	// constraints are refreshed immediately before opening exposure.
	notional, stop, tp, _, err := kernel.SizeRuleBasedLong(equity, available, marginUsed, price, d.RuleATR14)
	if err != nil {
		return err
	}
	d.PositionSizeUSD = math.Min(d.PositionSizeUSD, notional)
	d.StopLoss, d.TakeProfit = stop, tp
	d.RiskUSD = d.PositionSizeUSD * (price - stop) / price
	return nil
}

// Persisted opening orders prevent a closed signal from being consumed twice,
// including after a process restart. An unreadable audit history blocks entry.
func (at *AutoTrader) ruleBasedThrottleReason(d kernel.Decision, now time.Time) string {
	if !isOpenAction(d.Action) {
		return ""
	}
	if at.store == nil {
		return "indicator entry requires persistent order history"
	}
	if d.RuleSignalTimeMS != now.Truncate(4*time.Hour).UnixMilli() {
		return "indicator signal candle expired"
	}
	orders, err := at.recentOrders(100)
	if err != nil {
		return "cannot read indicator entry history"
	}
	for _, order := range orders {
		if order != nil && order.CreatedAt >= d.RuleSignalTimeMS && !isCanceledOrder(order) &&
			isOpenAction(order.OrderAction) && market.Normalize(order.Symbol) == d.Symbol {
			return "indicator signal has already been used for this market"
		}
	}
	return ""
}
