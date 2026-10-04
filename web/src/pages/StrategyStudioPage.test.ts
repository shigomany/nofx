import { describe, expect, it } from 'vitest'
import { simplifyConfig } from './StrategyStudioPage'
import type { StrategyConfig } from '../types'

describe('simplifyConfig', () => {
  it('preserves rule-based source, indicators, and multiple timeframes', () => {
    const config: StrategyConfig = {
      strategy_type: 'ai_trading',
      language: 'en',
      ai_config: {
        rule_based: { preset: 'rsi_pullback' },
        coin_source: {
          source_type: 'static',
          static_coins: ['BTCUSDT', 'ETHUSDT'],
          use_ai500: false,
          use_oi_top: false,
          use_oi_low: false,
        },
        indicators: {
          klines: {
            primary_timeframe: '4h',
            primary_count: 30,
            longer_timeframe: '1d',
            longer_count: 30,
            enable_multi_timeframe: true,
            selected_timeframes: ['4h', '1d'],
          },
          enable_raw_klines: true,
          enable_ema: true,
          enable_macd: false,
          enable_rsi: true,
          enable_atr: true,
          enable_boll: false,
          enable_volume: true,
          enable_oi: false,
          enable_funding_rate: true,
          ema_periods: [20, 50],
          rsi_periods: [14],
          atr_periods: [14],
        },
        risk_control: {
          max_positions: 1,
          btc_eth_max_leverage: 2,
          altcoin_max_leverage: 2,
          btc_eth_max_position_value_ratio: 0.5,
          altcoin_max_position_value_ratio: 0.5,
          max_margin_usage: 0.3,
          min_position_size: 12,
          min_risk_reward_ratio: 3,
          min_confidence: 0,
        },
      },
    }

    const result = simplifyConfig(config)

    expect(result.ai_config?.rule_based).toEqual({ preset: 'rsi_pullback' })
    expect(result.ai_config?.coin_source.source_type).toBe('static')
    expect(result.ai_config?.coin_source.static_coins).toEqual([
      'BTCUSDT',
      'ETHUSDT',
    ])
    expect(result.ai_config?.risk_control.min_confidence).toBe(0)
    expect(result.ai_config?.indicators).toMatchObject({
      enable_ema: true,
      enable_rsi: true,
      enable_atr: true,
      enable_volume: true,
      enable_funding_rate: true,
      ema_periods: [20, 50],
      rsi_periods: [14],
      atr_periods: [14],
      klines: {
        primary_timeframe: '4h',
        longer_timeframe: '1d',
        enable_multi_timeframe: true,
        selected_timeframes: ['4h', '1d'],
      },
    })
  })
})
