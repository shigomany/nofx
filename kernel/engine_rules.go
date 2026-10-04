package kernel

import (
	"encoding/json"
	"fmt"
	"math"
	"nofx/market"
	"nofx/store"
	"strings"
	"time"
)

// Indicator presets deliberately use fixed, auditable risk limits. They do not
// inherit prompt-only constraints or use model confidence as a probability.
const RuleLeverage = 2

func finitePositive(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

// SizeRuleBasedLong plans loss before trading costs, with no minimum-order
// inflation. Exchange slippage can still change the eventual realized loss.
func SizeRuleBasedLong(equity, available, marginUsed, entry, atr float64) (notional, stop, takeProfit, risk float64, err error) {
	if !finitePositive(equity) || !finitePositive(available) || !finitePositive(entry) || !finitePositive(atr) ||
		marginUsed < 0 || math.IsNaN(marginUsed) || math.IsInf(marginUsed, 0) {
		return 0, 0, 0, 0, fmt.Errorf("invalid account or price data")
	}
	distance := math.Max(1.5*atr, entry*0.005)
	if distance/entry > 0.035 {
		return 0, 0, 0, 0, fmt.Errorf("ATR stop exceeds 3.5%%; entry skipped")
	}
	marginBudget := equity*0.30 - marginUsed
	if marginBudget <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("margin budget exhausted")
	}
	notional = math.Min(equity*0.01/(distance/entry), equity*0.5)
	notional = math.Min(notional, marginBudget*RuleLeverage*0.98)
	notional = math.Min(notional, available/(1.01/RuleLeverage+0.001)*0.98)
	if !finitePositive(notional) || notional < 12 {
		return 0, 0, 0, 0, fmt.Errorf("safe size below $12 minimum; entry skipped")
	}
	return notional, entry - distance, entry + 3*distance, notional * distance / entry, nil
}

// getRuleBasedDecision bypasses all paid sources and AI clients. Partial data
// is safe to inspect, but entries require valid native data for their market.
func getRuleBasedDecision(ctx *Context, cfg *store.StrategyConfig, now time.Time) (*FullDecision, error) {
	if ctx.Exchange != "hyperliquid" {
		return nil, fmt.Errorf("indicator presets support Hyperliquid only")
	}
	if cfg.RuleBased == nil || (cfg.RuleBased.Preset != "trend_following" && cfg.RuleBased.Preset != "rsi_pullback") {
		return nil, fmt.Errorf("unsupported indicator preset")
	}
	if ctx.MarketDataMap == nil {
		ctx.MarketDataMap = make(map[string]*market.Data)
		for _, symbol := range []string{"BTCUSDT", "ETHUSDT"} {
			data, err := market.GetHyperliquidWithTimeframes(symbol, []string{"4h", "1d"}, "4h", 30)
			if err == nil {
				ctx.MarketDataMap[symbol] = data
			}
		}
	}
	return EvaluateRuleBased(ctx, cfg.RuleBased.Preset, now)
}

// EvaluateRuleBased is pure so closed-bar behavior and sizing can be tested
// without exchange credentials, network calls, model requests, or orders.
func EvaluateRuleBased(ctx *Context, preset string, now time.Time) (*FullDecision, error) {
	if ctx == nil || ctx.Exchange != "hyperliquid" {
		return nil, fmt.Errorf("indicator presets support Hyperliquid only")
	}
	if preset != "trend_following" && preset != "rsi_pullback" {
		return nil, fmt.Errorf("unsupported indicator preset %q", preset)
	}
	out := &FullDecision{
		Timestamp:    now,
		SystemPrompt: "Deterministic Hyperliquid indicator rules. Closed 4h/1d candles only; no AI or paid signal requests. Confidence 100 means rules passed, not a win probability.",
	}
	for _, pos := range ctx.Positions {
		symbol := market.Normalize(pos.Symbol)
		if symbol != "BTCUSDT" && symbol != "ETHUSDT" {
			continue // Never take ownership of unrelated exposure.
		}
		d := Decision{Symbol: symbol, Action: "hold", Reasoning: "Keep native exchange TP/SL; no position additions."}
		four, day, err := ruleSeries(ctx.MarketDataMap[symbol], now)
		if err != nil {
			d.Reasoning = "Missing/stale market data; keep existing exchange TP/SL."
		} else if strings.EqualFold(pos.Side, "long") {
			i, j := len(four.Klines)-1, len(day.Klines)-1
			if day.EMA20Values[j] <= day.EMA50Values[j] || day.Klines[j].Close <= day.EMA50Values[j] || four.Klines[i].Close < four.EMA50Values[i] {
				d.Action = "close_long"
				d.Reasoning = "Closed-candle trend invalidation: daily trend filter failed or 4h close below EMA50."
			}
		}
		out.Decisions = append(out.Decisions, d)
	}
	// Any existing position blocks new exposure, including unrelated markets.
	if len(ctx.Positions) == 0 && ctx.Account.PositionCount == 0 {
		for _, symbol := range []string{"BTCUSDT", "ETHUSDT"} {
			data := ctx.MarketDataMap[symbol]
			four, day, err := ruleSeries(data, now)
			if err != nil {
				out.Decisions = append(out.Decisions, Decision{Symbol: symbol, Action: "wait", Reasoning: err.Error()})
				continue
			}
			i, j := len(four.Klines)-1, len(day.Klines)-1
			trend := day.EMA20Values[j] > day.EMA50Values[j] && day.Klines[j].Close > day.EMA50Values[j]
			signal := false
			if preset == "trend_following" {
				priorHigh := 0.0
				for _, bar := range four.Klines[i-20 : i] {
					priorHigh = math.Max(priorHigh, bar.High)
				}
				signal = four.EMA20Values[i] > four.EMA50Values[i] && four.Klines[i].Close > priorHigh
			} else {
				signal = four.RSI14Values[i-1] <= 30 && four.RSI14Values[i] > 30 && four.Klines[i].Close < four.EMA20Values[i]
			}
			if !trend || !signal {
				out.Decisions = append(out.Decisions, Decision{Symbol: symbol, Action: "wait", Reasoning: "Closed-candle entry rules have not passed."})
				continue
			}
			atr := closedATR14(four.Klines)
			notional, stop, tp, risk, err := SizeRuleBasedLong(ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.MarginUsed, data.CurrentPrice, atr)
			if err != nil {
				out.Decisions = append(out.Decisions, Decision{Symbol: symbol, Action: "wait", Reasoning: err.Error()})
				continue
			}
			out.Decisions = append(out.Decisions, Decision{
				Symbol: symbol, Action: "open_long", Leverage: RuleLeverage,
				PositionSizeUSD: notional, StopLoss: stop, TakeProfit: tp, RiskUSD: risk,
				Confidence: 100, RuleATR14: atr, RuleSignalTimeMS: four.Klines[i].Time + int64(4*time.Hour/time.Millisecond),
				Reasoning: "Closed-candle rules passed; 100 denotes rule completion, not win probability. Execution rechecks price and risk; no DCA.",
			})
			break // Only one new position per cycle, deterministic BTC-first tie.
		}
	}
	if len(out.Decisions) == 0 {
		out.Decisions = []Decision{{Symbol: "BTCUSDT", Action: "wait", Reasoning: "Existing exposure blocks new entries."}}
	}
	raw, _ := json.Marshal(out.Decisions)
	out.RawResponse = string(raw)
	out.CoTTrace = "Fixed indicator rules evaluated locally."
	return out, nil
}

func ruleSeries(data *market.Data, now time.Time) (*market.TimeframeSeriesData, *market.TimeframeSeriesData, error) {
	if data == nil || !finitePositive(data.CurrentPrice) {
		return nil, nil, fmt.Errorf("missing native Hyperliquid market data")
	}
	four, err := closedRuleSeries(data.TimeframeData["4h"], 4*time.Hour, now)
	if err != nil {
		return nil, nil, err
	}
	day, err := closedRuleSeries(data.TimeframeData["1d"], 24*time.Hour, now)
	return four, day, err
}

func closedRuleSeries(series *market.TimeframeSeriesData, interval time.Duration, now time.Time) (*market.TimeframeSeriesData, error) {
	if series == nil || len(series.Klines) < 22 || len(series.EMA20Values) != len(series.Klines) || len(series.EMA50Values) != len(series.Klines) || len(series.RSI14Values) != len(series.Klines) {
		return nil, fmt.Errorf("insufficient or unaligned indicator history")
	}
	n := 0
	step := interval.Milliseconds()
	for i, bar := range series.Klines {
		if bar.Time <= 0 || bar.Time%step != 0 || (i > 0 && bar.Time-series.Klines[i-1].Time != step) ||
			!finitePositive(bar.Open) || !finitePositive(bar.High) || !finitePositive(bar.Low) || !finitePositive(bar.Close) ||
			bar.High < math.Max(bar.Open, bar.Close) || bar.Low > math.Min(bar.Open, bar.Close) ||
			!finitePositive(series.EMA20Values[i]) || !finitePositive(series.EMA50Values[i]) ||
			math.IsNaN(series.RSI14Values[i]) || math.IsInf(series.RSI14Values[i], 0) || series.RSI14Values[i] < 0 || series.RSI14Values[i] > 100 {
			return nil, fmt.Errorf("invalid indicator candles")
		}
		if bar.Time > now.UnixMilli() {
			return nil, fmt.Errorf("future market candle")
		}
		if bar.Time+step <= now.UnixMilli() {
			n = i + 1
		}
	}
	// Latest closed bar must be the immediately preceding completed interval.
	if n < 21 || now.UnixMilli()-(series.Klines[n-1].Time+step) >= step {
		return nil, fmt.Errorf("stale or insufficient closed candles")
	}
	copy := *series
	copy.Klines = series.Klines[:n]
	copy.EMA20Values = series.EMA20Values[:n]
	copy.EMA50Values = series.EMA50Values[:n]
	copy.RSI14Values = series.RSI14Values[:n]
	return &copy, nil
}

// Simple mean of 14 closed true ranges; the forming bar is excluded.
func closedATR14(bars []market.KlineBar) float64 {
	total := 0.0
	for i := len(bars) - 14; i < len(bars); i++ {
		bar := bars[i]
		previous := bars[i-1].Close
		total += math.Max(bar.High-bar.Low, math.Max(math.Abs(bar.High-previous), math.Abs(bar.Low-previous)))
	}
	return total / 14
}
