package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nofx/internal/codexgateway"
	"nofx/mcp"
)

func TestCodexForwardsOnlyTextAndCompletedAnswers(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		_, _ = w.Write([]byte(`{"content":{"response":"hold"},"usage":{}}`))
	}))
	defer server.Close()
	t.Setenv("CODEX_GATEWAY_URL", server.URL)
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN", strings.Repeat("x", 32))
	client := mcp.NewAIClientByProvider(mcp.ProviderCodex)
	if client == nil {
		t.Fatal("Codex is not registered")
	}
	client.SetAPIKey(codexgateway.ProfileID("alice"), "https://ignored.invalid", "selected-model")
	var chunks []string
	result, err := client.CallWithRequestStream(&mcp.Request{Ctx: context.Background(), Messages: []mcp.Message{mcp.NewSystemMessage("policy"), mcp.NewUserMessage("input")}}, func(s string) { chunks = append(chunks, s) })
	if err != nil || result != "hold" || len(chunks) != 1 || chunks[0] != "hold" {
		t.Fatalf("invalid completion: %v", err)
	}
	if received["model"] != "selected-model" || received["system"] != "policy\n" {
		t.Fatal("wrong model or policy")
	}
	if strings.Contains(received["user"].(string), strings.Repeat("x", 32)) {
		t.Fatal("credential sent to model")
	}
}

func TestCodexRejectsToolExecutionAndUnavailableGateway(t *testing.T) {
	client := mcp.NewAIClientByProvider(mcp.ProviderCodex)
	if _, err := client.CallWithRequest(&mcp.Request{Tools: []mcp.Tool{{Type: "function"}}}); err == nil {
		t.Fatal("tools accepted")
	}
	if _, err := client.CallWithRequest(&mcp.Request{Messages: []mcp.Message{{Role: "tool", Content: "secret"}}}); err == nil {
		t.Fatal("tool result accepted")
	}
	t.Setenv("CODEX_GATEWAY_URL", "")
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN", "")
	var called bool
	if _, err := client.CallWithRequestStream(&mcp.Request{}, func(string) { called = true }); err == nil || called {
		t.Fatal("published failed decision")
	}
}
