# Native Hyperliquid indicator presets

Two private, inactive templates are added to each account: **NOFX BTC/ETH
Trend** and **NOFX RSI Pullback**. Existing strategies, active selection, trader
bindings, and running settings are preserved. Adding a template does not start
a trader. Select it deliberately in the trader configuration before launching.

These experimental adaptations take inspiration from the trend/oversold entry
ideas in [Veles indicator presets](https://help.veles.finance/ru/platform/components/presets/),
including ETH SIGNAL. They are not copies of Veles DCA templates: their
indicators, intervals, exits, leverage and sizing differ. Veles results do not
establish these adaptations' profitability. There is no validated historical
or live PnL for either preset yet.

## Rules

Both use native Hyperliquid mainnet BTC and ETH candles, warmed with 200 bars.
Decisions use completed UTC 4h and daily candles. A forming candle cannot trigger
an entry or trend exit. Missing, invalid, gapped, future or stale indicator data
prevents new entries. No AI request, Vergex board, or paid NofxOS source is used.
The current trader UI still associates an enabled model record with a trader;
for these presets that record is metadata and incurs no inference calls or
AI charge records. AI wallet funding checks are skipped.

- Daily filter: EMA20 > EMA50 and daily close > EMA50.
- Trend entry: 4h EMA20 > EMA50 and closed 4h close above the highs of the prior
  20 closed 4h candles, excluding the signal candle.
- RSI entry: 4h RSI14 crosses from <=30 to >30, with the closed 4h close below
  EMA20, while the daily filter passes.
- Exit: exchange stop or take profit, or a closed-candle failure of the daily
  filter or 4h close below EMA50.
- Long only, no averaging, no martingale, at most one position across the
  account. Unrelated positions block entries and are not closed by this engine.
- When both markets qualify, BTC is selected first. Persistent order history
  prevents consuming the same market's signal twice after a restart; the
  existing 4h reentry cooldown also applies.

## Fixed risk limits

Leverage is 2x. Notional cannot exceed 50% of equity; total margin is capped at
30% of equity with an additional sizing buffer. At $45 equity, maximum notional
is $22.50, with smaller sizes when the risk or available funds require it.

The stop distance is max(1.5 × ATR14, 0.5% of entry price). ATR14 here is the
simple mean of the last 14 closed true ranges. Entries are skipped when that
distance exceeds 3.5%. Take profit is three stop distances above entry.
Position sizing caps planned stop loss at 1% of equity before costs ($0.45 at
$45). Safe sizes below the $12 minimum are skipped rather than increased.

Execution refreshes native mid price, equity, available funds and margin before
submitting the order, rechecks the signal candle, and can reduce size. Native
exchange TP/SL are mandatory; failure to install either invokes the existing
unprotected-position close path. Slippage, fees, funding, gaps, API failures and
liquidations can make realized losses differ from planned losses. Confidence
100 on a native decision indicates that fixed rules passed, not a 100% win rate.

Before relying on either preset, evaluate it with fees, funding and slippage in
a leakage-free out-of-sample backtest and observe a forward dry run. Unit tests
verify implementation and risk constraints; they do not demonstrate an edge.
