package api

import (
	"nofx/store"
	"testing"
)

func TestRuleBasedPreflightNeverQueriesAIWallet(t *testing.T) {
	original := queryAIWalletBalance
	queryAIWalletBalance = func(string) (float64, error) { t.Fatal("native preset queried paid AI wallet"); return 0, nil }
	t.Cleanup(func() { queryAIWalletBalance = original })
	strategy := &store.Strategy{}
	cfg := store.GetRuleBasedStrategyConfig(store.RuleBasedPresetTrendFollowing)
	if err := strategy.SetConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	result := (&Server{}).runLaunchPreflight("test", &store.AIModel{Enabled: true}, nil, strategy, true)
	for _, id := range []string{launchCheckAIWallet, launchCheckAIWalletFunds} {
		if got := findCheck(t, result.Checks, id); got.Status != launchCheckStatusSkipped {
			t.Fatalf("%s not skipped: %+v", id, got)
		}
	}
	if result.Ready {
		t.Fatal("missing exchange must still block launch")
	}
	if result.MinAIFeeUSDC != 0 {
		t.Fatal("native strategy has AI funding requirement")
	}
	for _, model := range []*store.AIModel{nil, {Enabled: false}} {
		result = (&Server{}).runLaunchPreflight("test", model, nil, strategy, true)
		if findCheck(t, result.Checks, launchCheckAIModel).Status != launchCheckStatusFailed {
			t.Fatal("missing/disabled model metadata accepted")
		}
	}
	cfg.RuleBased.Preset = "unknown"
	if err := strategy.SetConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	result = (&Server{}).runLaunchPreflight("test", nil, nil, strategy, true)
	if findCheck(t, result.Checks, "strategy_config").Status != launchCheckStatusFailed {
		t.Fatal("unsupported preset accepted")
	}
}
