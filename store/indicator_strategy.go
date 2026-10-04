package store

const (
	RuleBasedPresetTrendFollowing = "trend_following"
	RuleBasedPresetRSIPullback    = "rsi_pullback"
)

// RuleBasedStrategyConfig selects a deterministic, code-enforced strategy.
type RuleBasedStrategyConfig struct {
	Preset string `json:"preset"`
}

// IsSupportedRuleBasedPreset reports whether preset has an implementation in
// the deterministic strategy engine. Unknown values are intentionally retained
// in persisted configs so the engine can reject them instead of falling back.
func IsSupportedRuleBasedPreset(preset string) bool {
	switch preset {
	case RuleBasedPresetTrendFollowing, RuleBasedPresetRSIPullback:
		return true
	default:
		return false
	}
}

// GetRuleBasedStrategyConfig returns the fixed conservative configuration for
// a deterministic indicator preset. The caller must reject unsupported presets.
func GetRuleBasedStrategyConfig(preset string) StrategyConfig {
	return StrategyConfig{
		StrategyType: "ai_trading",
		Language:     "en",
		RuleBased:    &RuleBasedStrategyConfig{Preset: preset},
		CoinSource: CoinSourceConfig{
			SourceType:  "static",
			StaticCoins: []string{"BTCUSDT", "ETHUSDT"},
		},
		Indicators: IndicatorConfig{
			Klines: KlineConfig{
				PrimaryTimeframe:     "4h",
				PrimaryCount:         30,
				LongerTimeframe:      "1d",
				LongerCount:          30,
				EnableMultiTimeframe: true,
				SelectedTimeframes:   []string{"4h", "1d"},
			},
			EnableRawKlines: true,
			EnableEMA:       true,
			EnableRSI:       true,
			EnableATR:       true,
			EMAPeriods:      []int{20, 50},
			RSIPeriods:      []int{14},
			ATRPeriods:      []int{14},
		},
		RiskControl: RiskControlConfig{
			MaxPositions:                 1,
			BTCETHMaxLeverage:            2,
			AltcoinMaxLeverage:           2,
			BTCETHMaxPositionValueRatio:  0.5,
			AltcoinMaxPositionValueRatio: 0.5,
			MaxMarginUsage:               0.3,
			MinPositionSize:              12,
			MinRiskRewardRatio:           3,
			MinConfidence:                100,
		},
	}
}
