package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nofx/config"
	"nofx/internal/codexgateway"
	"nofx/manager"
	"nofx/store"

	"github.com/gin-gonic/gin"
)

func TestCodexCatalogAndTestHonorModelReasoningEffort(t *testing.T) {
	generated := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/account"):
			_, _ = w.Write([]byte(`{"account":{"type":"chatgpt","planType":"pro"}}`))
		case strings.HasSuffix(r.URL.Path, "/models"):
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-6.1-sol","model":"gpt-6.1-sol","displayName":"GPT-6.1 Sol","defaultReasoningEffort":"medium","supportedReasoningEfforts":[{"reasoningEffort":"low"},{"reasoningEffort":"high"},{"reasoningEffort":"none"},{"reasoningEffort":"ultra"}],"privateField":"must-not-leak"}]}`))
		case strings.HasSuffix(r.URL.Path, "/generate"):
			generated++
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["effort"] != "high" || body["model"] != "gpt-6.1-sol" {
				t.Error("test ignored selected effort/model")
			}
			_, _ = w.Write([]byte(`{"content":{"response":"OK"}}`))
		default:
			t.Errorf("unexpected gateway request: %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	t.Setenv("CODEX_GATEWAY_URL", upstream.URL)
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN", strings.Repeat("x", 32))
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN_FILE", "")
	s := &Server{}
	r := gin.New()
	r.GET("/catalog", func(c *gin.Context) { c.Set("user_id", "alice"); s.handleCodexModels(c) })
	r.POST("/test", func(c *gin.Context) { c.Set("user_id", "alice"); s.handleCodexTest(c) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/catalog", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "defaultReasoningEffort") || strings.Contains(w.Body.String(), "must-not-leak") || strings.Contains(w.Body.String(), `"ultra"`) || strings.Contains(w.Body.String(), `"none"`) {
		t.Fatalf("unsafe or incomplete catalog: %s", w.Body)
	}
	for _, tc := range []struct {
		model, effort string
		status        int
	}{
		{"gpt-6.1-sol", "high", http.StatusOK},
		{"gpt-6.1-sol", "none", http.StatusBadRequest},
		{"gpt-6.1-sol", "max", http.StatusBadRequest},
		{"gpt-6-astra", "high", http.StatusBadRequest},
		{"gpt-6.1-sol", "ultra", http.StatusBadRequest},
	} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/test", strings.NewReader(`{"model":"`+tc.model+`","effort":"`+tc.effort+`"}`)))
		if w.Code != tc.status {
			t.Fatalf("%s/%s returned %d: %s", tc.model, tc.effort, w.Code, w.Body)
		}
	}
	if generated != 1 {
		t.Fatal("generated using an unavailable model or effort")
	}
}

func TestCodexSavePersistsEffortAndRejectsUnavailableSelection(t *testing.T) {
	t.Setenv("JWT_SECRET", strings.Repeat("test-only-", 8))
	st, err := store.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.Get()
	previous := cfg.TransportEncryption
	cfg.TransportEncryption = false
	t.Cleanup(func() { cfg.TransportEncryption = previous })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/account") {
			_, _ = w.Write([]byte(`{"account":{"type":"chatgpt"}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-6.1-sol","supportedReasoningEfforts":[{"reasoningEffort":"low"},{"reasoningEffort":"high"}]}]}`))
		}
	}))
	defer upstream.Close()
	t.Setenv("CODEX_GATEWAY_URL", upstream.URL)
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN", strings.Repeat("x", 32))
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN_FILE", "")
	s := &Server{store: st, traderManager: manager.NewTraderManager()}
	r := gin.New()
	r.PUT("/models", func(c *gin.Context) { c.Set("user_id", "alice"); s.handleUpdateModelConfigs(c) })
	for _, tc := range []struct {
		payload string
		status  int
	}{
		{`{"enabled":true,"custom_model_name":"gpt-6.1-sol","reasoning_effort":"high"}`, http.StatusOK},
		{`{"enabled":true,"custom_model_name":"gpt-6.1-sol","reasoning_effort":"max"}`, http.StatusBadRequest},
		{`{"enabled":true,"custom_model_name":"not-in-catalog","reasoning_effort":"high"}`, http.StatusBadRequest},
		{`{"enabled":false,"custom_model_name":"gpt-6.1-sol"}`, http.StatusOK},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("PUT", "/models", strings.NewReader(`{"models":{"codex":`+tc.payload+`}}`)))
		if w.Code != tc.status {
			t.Fatalf("save returned %d: %s", w.Code, w.Body)
		}
		model, err := st.AIModel().Get("alice", "alice_codex")
		if err != nil || model.ReasoningEffort != "high" || model.CustomModelName != "gpt-6.1-sol" {
			t.Fatalf("save changed persisted choice: %v", err)
		}
	}
}

func TestCodexModelSaveCannotChooseAnotherTenant(t *testing.T) {
	st, err := store.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	t.Setenv("JWT_SECRET", strings.Repeat("test-only-", 8))
	cfg := config.Get()
	previous := cfg.TransportEncryption
	cfg.TransportEncryption = false
	t.Cleanup(func() { cfg.TransportEncryption = previous })
	s := &Server{store: st, traderManager: manager.NewTraderManager()}
	if err := st.AIModel().Create("alice", "named-model", "Codex", "codex", false, "", ""); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.PUT("/models", func(c *gin.Context) { c.Set("user_id", "alice"); s.handleUpdateModelConfigs(c) })
	for _, id := range []string{"codex", "arbitrary_codex", "named-model"} {
		body := `{"models":{"` + id + `":{"enabled":false,"api_key":"` + codexgateway.ProfileID("bob") + `"}}}`
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("PUT", "/models", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("save %s failed: %s", id, w.Body)
		}
		storedID := id
		if id == "codex" {
			storedID = "named-model" // legacy provider lookup resolves the existing row
		}
		model, err := st.AIModel().Get("alice", storedID)
		if err != nil || string(model.APIKey) != codexgateway.ProfileID("alice") {
			t.Fatalf("save %s accepted another profile: %v", id, err)
		}
	}
	for _, body := range []string{
		`{"models":{"arbitrary_codex":{"enabled":false,"custom_api_url":"https://example.test"}}}`,
		`{"models":{"zai":{"enabled":false,"custom_api_url":"https://api.z.ai/api/coding/paas/v4"}}}`,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("PUT", "/models", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("unsafe endpoint accepted: %s", w.Body)
		}
	}
}

func TestCodexDisconnectRejectsRunningTraderAndOnlyLogsOutOwnProfile(t *testing.T) {
	st, err := store.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, user := range []string{"alice", "bob"} {
		if err := st.AIModel().Create(user, user+"_codex", "Codex", "codex", true, codexgateway.ProfileID(user), ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Trader().Create(&store.Trader{ID: "active", UserID: "alice", Name: "Test", AIModelID: "alice_codex", ExchangeID: "test", InitialBalance: 100, IsRunning: true}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/profiles/"+codexgateway.ProfileID("alice")+"/logout" {
			t.Errorf("wrong profile: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"disconnected":true}`))
	}))
	defer gateway.Close()
	t.Setenv("CODEX_GATEWAY_URL", gateway.URL)
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN", strings.Repeat("x", 32))
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN_FILE", "")
	s := &Server{store: st}
	r := gin.New()
	r.POST("/disconnect", func(c *gin.Context) { c.Set("user_id", "alice"); s.handleCodexDisconnect(c) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/disconnect", nil))
	if w.Code != http.StatusConflict || calls != 0 {
		t.Fatal("disconnected a running trader")
	}
	if err := st.Trader().UpdateStatus("alice", "active", false); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/disconnect?profile_id="+codexgateway.ProfileID("bob"), nil))
	if w.Code != http.StatusOK || calls != 1 {
		t.Fatalf("disconnect failed: %s", w.Body)
	}
	for _, user := range []string{"alice", "bob"} {
		model, err := st.AIModel().Get(user, user+"_codex")
		if err != nil || model.Enabled != (user == "bob") {
			t.Fatalf("wrong model state for %s", user)
		}
	}
}

func TestCodexStatusUsesAuthenticatedTenantNotBrowserProfile(t *testing.T) {
	profile := codexgateway.ProfileID("alice")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/profiles/"+profile+"/") {
			t.Errorf("wrong tenant: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "login/status") {
			_, _ = w.Write([]byte(`{"status":"pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"account":{"type":"chatgpt","planType":"pro","access_token":"must-not-leak"}}`))
	}))
	defer server.Close()
	t.Setenv("CODEX_GATEWAY_URL", server.URL)
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN", strings.Repeat("x", 32))
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN_FILE", "")
	r := gin.New()
	s := &Server{}
	r.GET("/status", func(c *gin.Context) { c.Set("user_id", "alice"); s.handleCodexStatus(c) })
	for _, tc := range []struct{ query, want string }{
		{"?profile_id=" + codexgateway.ProfileID("bob"), `"connected":true`},
		{"?login_id=pending&profile_id=" + codexgateway.ProfileID("bob"), `"connected":false`},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/status"+tc.query, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.want) || strings.Contains(w.Body.String(), "must-not-leak") {
			t.Fatalf("unsafe status: %s", w.Body)
		}
	}
}

func TestCodexHandlersRequireAuthenticatedUser(t *testing.T) {
	r := gin.New()
	s := &Server{}
	r.POST("/connect", s.handleCodexConnect)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/connect", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status %d", w.Code)
	}
}
