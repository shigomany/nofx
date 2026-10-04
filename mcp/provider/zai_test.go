package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nofx/mcp"
)

func TestZAIUpstreamErrorDoesNotExposeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"echoed-test-key-and-private-prompt"}`))
	}))
	defer server.Close()
	client := NewZAIClientWithOptions().(*ZAIClient)
	client.SetAPIKey("test-only-key", server.URL, DefaultZAIModel)
	_, err := client.Call("policy", "input")
	if err == nil || strings.Contains(err.Error(), "private-prompt") || strings.Contains(err.Error(), "echoed-test-key") {
		t.Fatalf("unsafe upstream error: %v", err)
	}
}

func TestZAIProviderRegistrationAndDefaults(t *testing.T) {
	client := mcp.NewAIClientByProvider("zai")
	if client == nil {
		t.Fatal("zai provider is not registered")
	}

	zai, ok := client.(*ZAIClient)
	if !ok {
		t.Fatalf("provider type = %T, want *ZAIClient", client)
	}
	if zai.Provider != "zai" {
		t.Errorf("provider = %q, want zai", zai.Provider)
	}
	if zai.BaseURL != DefaultZAIBaseURL {
		t.Errorf("base URL = %q, want %q", zai.BaseURL, DefaultZAIBaseURL)
	}
	if zai.Model != DefaultZAIModel {
		t.Errorf("model = %q, want %q", zai.Model, DefaultZAIModel)
	}
}

func TestZAISetAPIKeyAuthAndOverrides(t *testing.T) {
	client := NewZAIClientWithOptions().(*ZAIClient)
	client.SetAPIKey("zai-test-key", "https://example.test/v4", "custom-glm")

	if client.BaseURL != "https://example.test/v4" || client.Model != "custom-glm" {
		t.Fatalf("overrides = (%q, %q), want custom URL and model", client.BaseURL, client.Model)
	}

	headers := make(http.Header)
	client.SetAuthHeader(headers)
	if got, want := headers.Get("Authorization"), "Bearer zai-test-key"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestZAIBuildersDisableThinking(t *testing.T) {
	client := NewZAIClientWithOptions().(*ZAIClient)

	legacy := client.BuildMCPRequestBody("system", "user")
	assertThinkingDisabled(t, legacy)

	request := &mcp.Request{
		Messages: []mcp.Message{mcp.NewUserMessage("user")},
		Stream:   true,
		Tools:    []mcp.Tool{{Function: mcp.FunctionDef{Name: "lookup"}}},
	}
	builder := client.BuildRequestBodyFromRequest(request)
	assertThinkingDisabled(t, builder)
	if got, ok := builder["stream"].(bool); !ok || !got {
		t.Errorf("stream = %#v, want true", builder["stream"])
	}
	if _, ok := builder["tools"]; !ok {
		t.Error("builder dropped tools")
	}
}

func assertThinkingDisabled(t *testing.T, body map[string]any) {
	t.Helper()
	thinking, ok := body["thinking"].(map[string]string)
	if !ok {
		t.Fatalf("thinking = %#v, want object", body["thinking"])
	}
	if thinking["type"] != "disabled" {
		t.Errorf("thinking.type = %q, want disabled", thinking["type"])
	}
}

func TestZAIResponseContentAndReasoning(t *testing.T) {
	client := NewZAIClientWithOptions().(*ZAIClient)
	body := []byte(`{"choices":[{"message":{"content":"answer","reasoning_content":"analysis"}}]}`)
	response, err := client.ParseMCPResponseFull(body)
	if err != nil {
		t.Fatalf("ParseMCPResponseFull() error = %v", err)
	}
	if response.Content != "answer" || response.ReasoningContent != "analysis" {
		t.Errorf("response = %+v, want content and reasoning", response)
	}

	encoded, err := json.Marshal(client.BuildMCPRequestBody("", "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) == 0 {
		t.Fatal("legacy request body encoded to empty JSON")
	}
}
