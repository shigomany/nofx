package store

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestGetRuleBasedStrategyConfigUsesFixedConservativeInvariants(t *testing.T) {
	for _, preset := range []string{RuleBasedPresetTrendFollowing, RuleBasedPresetRSIPullback} {
		cfg := GetRuleBasedStrategyConfig(preset)
		if cfg.StrategyType != "ai_trading" || cfg.Language != "en" {
			t.Fatalf("%s identity = %q/%q", preset, cfg.StrategyType, cfg.Language)
		}
		if cfg.RuleBased == nil || cfg.RuleBased.Preset != preset {
			t.Fatalf("%s rule_based = %+v", preset, cfg.RuleBased)
		}
		if cfg.CoinSource.SourceType != "static" || !reflect.DeepEqual(cfg.CoinSource.StaticCoins, []string{"BTCUSDT", "ETHUSDT"}) {
			t.Fatalf("%s coin source = %+v", preset, cfg.CoinSource)
		}
		if cfg.CoinSource.UseAI500 || cfg.CoinSource.UseOITop || cfg.CoinSource.UseOILow || cfg.CoinSource.UseHyperAll || cfg.CoinSource.UseHyperMain || cfg.CoinSource.VergexLimit != 0 {
			t.Fatalf("%s enables a dynamic or paid coin source: %+v", preset, cfg.CoinSource)
		}
		if got := cfg.Indicators; !got.EnableRawKlines || !got.EnableEMA || !got.EnableRSI || !got.EnableATR || got.EnableMACD || got.EnableBOLL || got.EnableVolume || got.EnableOI || got.EnableFundingRate || got.NofxOSAPIKey != "" || got.EnableQuantData {
			t.Fatalf("%s indicators = %+v", preset, got)
		}
		if !reflect.DeepEqual(cfg.Indicators.EMAPeriods, []int{20, 50}) || !reflect.DeepEqual(cfg.Indicators.RSIPeriods, []int{14}) || !reflect.DeepEqual(cfg.Indicators.ATRPeriods, []int{14}) {
			t.Fatalf("%s periods = %+v", preset, cfg.Indicators)
		}
		if got := cfg.RiskControl; got.MaxPositions != 1 || got.BTCETHMaxLeverage != 2 || got.AltcoinMaxLeverage != 2 || got.BTCETHMaxPositionValueRatio != 0.5 || got.AltcoinMaxPositionValueRatio != 0.5 || got.MaxMarginUsage != 0.3 || got.MinPositionSize != 12 || got.MinRiskRewardRatio != 3 || got.MinConfidence > 100 {
			t.Fatalf("%s risk = %+v", preset, got)
		}
	}
}

func TestRuleBasedStrategySchemaSupportsNestedLegacyAndMerge(t *testing.T) {
	cfg := GetRuleBasedStrategyConfig(RuleBasedPresetTrendFollowing)
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	aiConfig := payload["ai_config"].(map[string]any)
	if _, ok := aiConfig["rule_based"]; !ok {
		t.Fatalf("nested rule_based missing: %s", raw)
	}
	if _, ok := payload["rule_based"]; ok {
		t.Fatalf("unexpected top-level rule_based: %s", raw)
	}

	var legacy StrategyConfig
	if err := json.Unmarshal([]byte(`{"strategy_type":"ai_trading","rule_based":{"preset":"rsi_pullback"}}`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if legacy.RuleBased == nil || legacy.RuleBased.Preset != RuleBasedPresetRSIPullback {
		t.Fatalf("legacy rule_based = %+v", legacy.RuleBased)
	}

	merged, err := MergeStrategyConfig(cfg, map[string]any{"rule_based": map[string]any{"preset": RuleBasedPresetRSIPullback}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if merged.RuleBased == nil || merged.RuleBased.Preset != RuleBasedPresetRSIPullback {
		t.Fatalf("merged rule_based = %+v", merged.RuleBased)
	}
}

func TestClampLimitsRestoresKnownRuleBasedInvariantsAndKeepsUnknownPreset(t *testing.T) {
	cfg := GetRuleBasedStrategyConfig(RuleBasedPresetTrendFollowing)
	cfg.CoinSource.StaticCoins = []string{"DOGEUSDT"}
	cfg.RiskControl.MaxPositions = 8
	cfg.RiskControl.BTCETHMaxLeverage = 20
	cfg.Indicators.EnableEMA = false
	cfg.PublishConfig = &PublishStrategyConfig{IsPublic: true, ConfigVisible: false}
	cfg.ClampLimits()
	want := GetRuleBasedStrategyConfig(RuleBasedPresetTrendFollowing)
	want.PublishConfig = cfg.PublishConfig
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("known preset was not restored:\n got: %+v\nwant: %+v", cfg, want)
	}

	unknown := GetRuleBasedStrategyConfig("future_preset")
	unknown.ClampLimits()
	if unknown.RuleBased == nil || unknown.RuleBased.Preset != "future_preset" {
		t.Fatalf("unknown preset must be retained, got %+v", unknown.RuleBased)
	}
	if IsSupportedRuleBasedPreset("future_preset") {
		t.Fatal("unknown preset reported as supported")
	}
}
