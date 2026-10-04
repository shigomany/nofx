import { ExternalLink, Loader2, Save, ShieldCheck } from 'lucide-react'
import type { AIStrategyConfig, Strategy } from '../../types'
import { ROUTES } from '../../router/paths'

interface Props {
  strategy: Strategy
  config: AIStrategyConfig
  language: string
  saving: boolean
  hasChanges: boolean
  onNameChange: (name: string) => void
  onDescriptionChange: (description: string) => void
  onSave: () => void
}

const copy = (language: string, zh: string, en: string) =>
  language === 'zh' ? zh : en

export function IndicatorStrategyPanel({
  strategy,
  config,
  language,
  saving,
  hasChanges,
  onNameChange,
  onDescriptionChange,
  onSave,
}: Props) {
  const preset = config.rule_based?.preset
  const isTrend = preset === 'trend_following'
  const symbols = config.coin_source.static_coins || ['BTCUSDT', 'ETHUSDT']
  const timeframes = config.indicators.klines.selected_timeframes || [
    config.indicators.klines.primary_timeframe,
  ]
  const risk = config.risk_control

  return (
    <div className="space-y-4">
      <section className="rounded-lg border border-[rgba(26,24,19,0.14)] bg-nofx-bg-lighter p-5">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0 flex-1">
            <input
              aria-label="Strategy name"
              value={strategy.name}
              onChange={(event) => onNameChange(event.target.value)}
              className="w-full bg-transparent text-xl font-semibold text-nofx-text outline-none"
            />
            <input
              aria-label="Strategy description"
              value={strategy.description || ''}
              onChange={(event) => onDescriptionChange(event.target.value)}
              className="mt-2 w-full bg-transparent text-sm text-nofx-text-muted outline-none"
            />
          </div>
          <button
            type="button"
            onClick={onSave}
            disabled={saving || !hasChanges}
            className="inline-flex items-center gap-2 rounded-lg bg-nofx-gold px-4 py-2 text-sm font-semibold text-nofx-bg disabled:cursor-not-allowed disabled:opacity-45"
          >
            {saving ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Save className="h-4 w-4" />
            )}
            {copy(language, 'Save', 'Save')}
          </button>
        </div>
      </section>

      <section className="rounded-lg border border-nofx-gold/30 bg-nofx-gold/5 p-5">
        <div className="flex items-center gap-2 text-lg font-semibold text-nofx-text">
          <ShieldCheck className="h-5 w-5 text-nofx-gold" />
          {isTrend ? 'EMA trend breakout' : 'RSI pullback with trend filter'}
        </div>
        <p className="mt-2 text-sm leading-6 text-nofx-text-muted">
          Experimental deterministic strategy. It has no validated live PnL. The
          backend evaluates closed candles on Hyperliquid mainnet only and does
          not send AI requests for decisions. A confidence value of 100 means
          that the deterministic rule passed, not a 100% probability of a win.
        </p>
      </section>

      <div className="grid gap-4 xl:grid-cols-2">
        <section className="rounded-lg border border-[rgba(26,24,19,0.14)] bg-nofx-bg-lighter p-5">
          <h2 className="font-semibold text-nofx-text">Entry rules</h2>
          <ul className="mt-3 space-y-2 text-sm leading-6 text-nofx-text-muted">
            <li>
              Long only; daily EMA20 &gt; EMA50 and daily close &gt; EMA50.
            </li>
            <li>
              {isTrend
                ? '4h EMA20 > EMA50 and the closed candle breaks the prior 20-candle high.'
                : '4h RSI14 crosses above 30 while the 4h close remains below EMA20.'}
            </li>
            <li>No DCA or martingale; at most one open position.</li>
          </ul>
        </section>
        <section className="rounded-lg border border-[rgba(26,24,19,0.14)] bg-nofx-bg-lighter p-5">
          <h2 className="font-semibold text-nofx-text">Exit and risk</h2>
          <ul className="mt-3 space-y-2 text-sm leading-6 text-nofx-text-muted">
            <li>
              Stop: max(1.5 × closed ATR14, 0.5% of entry), capped at 3.5%.
            </li>
            <li>
              Close long if the daily trend filter fails or the closed 4h candle
              closes below EMA50. Exchange TP/SL remain mandatory.
            </li>
            <li>
              Planned loss: 1% of equity before costs; minimum notional $
              {risk.min_position_size}.
            </li>
            <li>
              {risk.altcoin_max_leverage}× leverage ·{' '}
              {(risk.max_margin_usage * 100).toFixed(0)}% max margin ·{' '}
              {risk.min_risk_reward_ratio}:1 reward/risk.
            </li>
          </ul>
        </section>
      </div>

      <section className="rounded-lg border border-[rgba(26,24,19,0.14)] bg-nofx-bg-lighter p-5">
        <h2 className="font-semibold text-nofx-text">Data and budget</h2>
        <div className="mt-3 grid gap-3 text-sm sm:grid-cols-2 xl:grid-cols-4">
          <div className="rounded-lg bg-nofx-bg-deeper p-3">
            <div className="text-nofx-text-muted">Markets</div>
            <div className="mt-1 font-mono text-nofx-text">
              {symbols.join(' · ')}
            </div>
          </div>
          <div className="rounded-lg bg-nofx-bg-deeper p-3">
            <div className="text-nofx-text-muted">Closed candles</div>
            <div className="mt-1 font-mono text-nofx-text">
              {timeframes.join(' · ')} ·{' '}
              {config.indicators.klines.primary_count} bars
            </div>
          </div>
          <div className="rounded-lg bg-nofx-bg-deeper p-3">
            <div className="text-nofx-text-muted">Max notional</div>
            <div className="mt-1 font-mono text-nofx-text">
              50% of equity · 22.5 USDC at 45 USDC
            </div>
          </div>
          <div className="rounded-lg bg-nofx-bg-deeper p-3">
            <div className="text-nofx-text-muted">Execution venue</div>
            <div className="mt-1 font-mono text-nofx-text">
              Hyperliquid mainnet · native TP/SL
            </div>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap gap-4 text-sm">
          <a
            className="inline-flex items-center gap-1 text-nofx-gold hover:underline"
            href="https://help.veles.finance/ru/platform/components/presets/"
            target="_blank"
            rel="noreferrer"
          >
            {isTrend
              ? 'Veles indicator presets inspiration'
              : 'Veles ETH SIGNAL inspiration'}{' '}
            <ExternalLink className="h-3.5 w-3.5" />
          </a>
          <a
            className="inline-flex items-center gap-1 text-nofx-gold hover:underline"
            href={ROUTES.traders}
          >
            Open Traders for explicit setup{' '}
            <ExternalLink className="h-3.5 w-3.5" />
          </a>
        </div>
      </section>
    </div>
  )
}
