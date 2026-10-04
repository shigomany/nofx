package api

import (
	"testing"

	"github.com/google/uuid"
	"nofx/store"
)

func TestCreateDefaultStrategiesUsesOneReadyToRunClaw402AndInactiveRulePresets(t *testing.T) {
	st, err := store.New(t.TempDir() + "/nofx.db")
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	s := &Server{store: st}
	userID := "user-us-stock-presets"
	if err := s.createDefaultStrategies(userID, "zh"); err != nil {
		t.Fatalf("createDefaultStrategies failed: %v", err)
	}

	strategies, err := st.Strategy().List(userID)
	if err != nil {
		t.Fatalf("List strategies failed: %v", err)
	}
	if len(strategies) != 3 {
		t.Fatalf("expected 3 default strategies, got %d", len(strategies))
	}

	byName := map[string]*store.Strategy{}
	activeCount := 0
	for _, strategy := range strategies {
		byName[strategy.Name] = strategy
		if strategy.IsActive {
			activeCount++
		}
		if strategy.Name == "Balanced Strategy" || strategy.Name == "Steady Strategy" || strategy.Name == "Aggressive Strategy" {
			t.Fatalf("legacy crypto-style default strategy still present: %s", strategy.Name)
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly one active strategy, got %d", activeCount)
	}

	defaultStrategy := byName["NOFX Claw402 Auto Strategy"]
	if defaultStrategy == nil || !defaultStrategy.IsActive {
		t.Fatalf("NOFX Claw402 Auto Strategy should exist and be active")
	}
	trendCfg, err := defaultStrategy.ParseConfig()
	if err != nil {
		t.Fatalf("default ParseConfig failed: %v", err)
	}
	if trendCfg.CoinSource.SourceType != "vergex_signal" || trendCfg.CoinSource.VergexLimit != 10 || trendCfg.CoinSource.VergexMarketType != "all" {
		t.Fatalf("default strategy should use the Claw402/Vergex all-market direction board, got %+v", trendCfg.CoinSource)
	}
	if trendCfg.CoinSource.UseAI500 || trendCfg.RiskControl.MaxPositions != store.AutopilotDefaultMaxPositions {
		t.Fatalf("default strategy should be Claw402/Vergex native with an 8-position book, got coin=%+v risk=%+v", trendCfg.CoinSource, trendCfg.RiskControl)
	}
	if trendCfg.RiskControl.BTCETHMaxLeverage != 10 || trendCfg.RiskControl.AltcoinMaxLeverage != 10 {
		t.Fatalf("default strategy should use 10x leverage for all Claw402 opens, got risk=%+v", trendCfg.RiskControl)
	}
	if trendCfg.RiskControl.BTCETHMaxPositionValueRatio != store.AutopilotMaxPositionValueRatio ||
		trendCfg.RiskControl.AltcoinMaxPositionValueRatio != store.AutopilotMaxPositionValueRatio ||
		trendCfg.RiskControl.MaxMarginUsage != 1.0 {
		t.Fatalf("default strategy should enforce a 5x-equity hard cap per position, got risk=%+v", trendCfg.RiskControl)
	}

	for name, preset := range map[string]string{
		"NOFX BTC/ETH Trend": store.RuleBasedPresetTrendFollowing,
		"NOFX RSI Pullback":  store.RuleBasedPresetRSIPullback,
	} {
		strategy := byName[name]
		if strategy == nil {
			t.Fatalf("missing rule-based preset %q", name)
		}
		if strategy.IsActive || strategy.IsPublic {
			t.Fatalf("rule-based preset %q must be inactive and private: %+v", name, strategy)
		}
		cfg, err := strategy.ParseConfig()
		if err != nil {
			t.Fatalf("parse %q: %v", name, err)
		}
		if cfg.RuleBased == nil || cfg.RuleBased.Preset != preset {
			t.Fatalf("rule-based preset %q config = %+v, want %q", name, cfg.RuleBased, preset)
		}
	}
}

func TestCreateDefaultStrategiesMigratesExistingTwoPositionAutopilot(t *testing.T) {
	st, err := store.New(t.TempDir() + "/nofx.db")
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	userID := "user-legacy-autopilot"
	legacyConfig := store.GetDefaultStrategyConfig("en")
	legacyConfig.RiskControl.MaxPositions = 2
	legacyConfig.RiskControl.BTCETHMaxPositionValueRatio = 5
	legacyConfig.RiskControl.AltcoinMaxPositionValueRatio = 5
	legacy := &store.Strategy{
		ID:          uuid.New().String(),
		UserID:      userID,
		Name:        "NOFX Claw402 Auto Strategy",
		Description: "legacy two-position config",
		IsActive:    true,
	}
	if err := legacy.SetConfig(&legacyConfig); err != nil {
		t.Fatalf("legacy SetConfig failed: %v", err)
	}
	if err := st.Strategy().Create(legacy); err != nil {
		t.Fatalf("create legacy strategy failed: %v", err)
	}

	s := &Server{store: st}
	if err := s.createDefaultStrategies(userID, "en"); err != nil {
		t.Fatalf("createDefaultStrategies failed: %v", err)
	}

	migrated, err := st.Strategy().Get(userID, legacy.ID)
	if err != nil {
		t.Fatalf("get migrated strategy failed: %v", err)
	}
	migratedConfig, err := migrated.ParseConfig()
	if err != nil {
		t.Fatalf("parse migrated strategy failed: %v", err)
	}
	if migratedConfig.RiskControl.MaxPositions != store.AutopilotDefaultMaxPositions ||
		migratedConfig.RiskControl.BTCETHMaxPositionValueRatio != store.AutopilotMaxPositionValueRatio ||
		migratedConfig.RiskControl.AltcoinMaxPositionValueRatio != store.AutopilotMaxPositionValueRatio {
		t.Fatalf("legacy strategy was not migrated: %+v", migratedConfig.RiskControl)
	}
}

func TestCreateDefaultStrategiesMigratesLegacyPresetsWithoutOverridingActiveCustom(t *testing.T) {
	st, err := store.New(t.TempDir() + "/nofx.db")
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	userID := "user-existing-custom"
	legacyCfg := store.GetDefaultStrategyConfig("zh")
	legacy := &store.Strategy{
		ID:          uuid.New().String(),
		UserID:      userID,
		Name:        "Balanced Strategy",
		Description: "legacy",
		IsActive:    false,
	}
	if err := legacy.SetConfig(&legacyCfg); err != nil {
		t.Fatalf("legacy SetConfig failed: %v", err)
	}
	if err := st.Strategy().Create(legacy); err != nil {
		t.Fatalf("create legacy failed: %v", err)
	}

	custom := &store.Strategy{
		ID:          uuid.New().String(),
		UserID:      userID,
		Name:        "aa",
		Description: "user custom active strategy",
		IsActive:    true,
	}
	if err := custom.SetConfig(&legacyCfg); err != nil {
		t.Fatalf("custom SetConfig failed: %v", err)
	}
	if err := st.Strategy().Create(custom); err != nil {
		t.Fatalf("create custom failed: %v", err)
	}

	existingRuleCfg := store.GetRuleBasedStrategyConfig(store.RuleBasedPresetTrendFollowing)
	existingRuleCfg.RiskControl.MaxPositions = 8
	existingRule := &store.Strategy{
		ID:          uuid.New().String(),
		UserID:      userID,
		Name:        "NOFX BTC/ETH Trend",
		Description: "user-edited native strategy description",
		IsActive:    true,
		IsPublic:    true,
	}
	if err := existingRule.SetConfig(&existingRuleCfg); err != nil {
		t.Fatalf("existing rule SetConfig failed: %v", err)
	}
	if err := st.Strategy().Create(existingRule); err != nil {
		t.Fatalf("create existing rule failed: %v", err)
	}

	s := &Server{store: st}
	if err := s.createDefaultStrategies(userID, "zh"); err != nil {
		t.Fatalf("createDefaultStrategies failed: %v", err)
	}
	if err := s.createDefaultStrategies(userID, "zh"); err != nil {
		t.Fatalf("second createDefaultStrategies should be idempotent: %v", err)
	}

	strategies, err := st.Strategy().List(userID)
	if err != nil {
		t.Fatalf("List strategies failed: %v", err)
	}
	byName := map[string]int{}
	activeNames := []string{}
	for _, strategy := range strategies {
		byName[strategy.Name]++
		if strategy.IsActive {
			activeNames = append(activeNames, strategy.Name)
		}
	}
	if byName["Balanced Strategy"] != 0 {
		t.Fatalf("legacy preset should be removed, got names=%+v", byName)
	}
	if byName["NOFX Claw402 Auto Strategy"] != 1 {
		t.Fatalf("expected exactly one NOFX Claw402 Auto Strategy, got names=%+v", byName)
	}
	if byName["NOFX BTC/ETH Trend"] != 1 || byName["NOFX RSI Pullback"] != 1 {
		t.Fatalf("expected exactly one of each rule-based preset, got names=%+v", byName)
	}
	if len(activeNames) != 2 {
		t.Fatalf("existing active strategies should be preserved, got %+v", activeNames)
	}
	for _, strategy := range strategies {
		if strategy.Name != "NOFX BTC/ETH Trend" {
			continue
		}
		if !strategy.IsActive || !strategy.IsPublic {
			t.Fatalf("existing native preset active/public state should be preserved: %+v", strategy)
		}
		if strategy.Description != "user-edited native strategy description" {
			t.Fatalf("existing native preset description was overwritten: %q", strategy.Description)
		}
		cfg, err := strategy.ParseConfig()
		if err != nil {
			t.Fatalf("parse synchronized rule preset: %v", err)
		}
		if cfg.RiskControl.MaxPositions != 8 {
			t.Fatalf("existing native preset config was overwritten: %+v", cfg.RiskControl)
		}
	}
}
