package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"nofx/store"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreateRuleTraderDoesNotRequireAIKey(t *testing.T) {
	st, err := store.New(t.TempDir() + "/nofx.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.AIModel().Create("user", "metadata", "Model metadata", "codex", true, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, preset := range []string{"trend_following", "ai", "unknown"} {
		cfg := store.GetRuleBasedStrategyConfig(preset)
		if preset == "ai" {
			cfg = store.GetDefaultStrategyConfig("en")
		}
		strategy := &store.Strategy{ID: preset, UserID: "user", Name: preset}
		if err := strategy.SetConfig(&cfg); err != nil {
			t.Fatal(err)
		}
		if err := st.Strategy().Create(strategy); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(CreateTraderRequest{Name: "Rules", AIModelID: "metadata", ExchangeID: "missing-exchange", StrategyID: preset})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("user_id", "user")
		c.Request = httptest.NewRequest("POST", "/api/traders", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		(&Server{store: st}).handleCreateTrader(c)
		if w.Code != 400 {
			t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
		}
		response := w.Body.String()
		switch preset {
		case "trend_following":
			if !strings.Contains(response, "exchange_not_found") || strings.Contains(response, "model_missing_credentials") {
				t.Fatalf("native requires model key: %s", response)
			}
		case "ai":
			if !strings.Contains(response, "model_missing_credentials") {
				t.Fatalf("AI credentials guard bypassed: %s", response)
			}
		case "unknown":
			if !strings.Contains(response, "unsupported strategy") {
				t.Fatalf("unknown native preset accepted: %s", response)
			}
		}
	}
}
