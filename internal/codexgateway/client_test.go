package codexgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProfilesAreStableAndTenantScoped(t *testing.T) {
	if ProfileID("alice") != ProfileID("alice") || ProfileID("alice") == ProfileID("bob") {
		t.Fatal("profile isolation failed")
	}
}

func TestGenerateKeepsCredentialsPrivateAndValidatesFinalOutput(t *testing.T) {
	profile := ProfileID("alice")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/profiles/"+profile+"/generate" {
			t.Errorf("wrong profile path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
			t.Error("missing service auth")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid request")
		}
		if body["effort"] != "high" || body["timeout_seconds"] != float64(35) {
			t.Error("wrong generation limits")
		}
		if strings.Contains(r.URL.String(), strings.Repeat("x", 32)) {
			t.Error("credential in URL")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":{"response":"<decision>hold</decision>"},"usage":{}}`))
	}))
	defer server.Close()
	client := &Client{URL: server.URL, Token: strings.Repeat("x", 32), HTTP: server.Client()}
	result, err := client.Generate(context.Background(), profile, "test-model", "policy", "input", 35*time.Second, "high")
	if err != nil || result.Content.Response != "<decision>hold</decision>" {
		t.Fatalf("unexpected response: %v", err)
	}
}

func TestFailureDoesNotExposeUpstreamBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("sensitive upstream token"))
	}))
	defer server.Close()
	client := &Client{URL: server.URL, Token: strings.Repeat("x", 32), HTTP: server.Client()}
	_, err := client.Account(context.Background(), ProfileID("alice"))
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("unsafe gateway error")
	}
}

func TestRejectsInvalidProfilesAndEmptyOutput(t *testing.T) {
	client := &Client{}
	if client.Request(context.Background(), "GET", "../../bob", "account", nil, nil) == nil {
		t.Fatal("accepted invalid profile")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"content":{}}`)) }))
	defer server.Close()
	client.URL = server.URL
	client.HTTP = server.Client()
	if _, err := client.Generate(context.Background(), ProfileID("alice"), "test", "policy", "input", time.Second, ""); err == nil {
		t.Fatal("accepted missing final output")
	}
}

func TestAccountRequiresChatGPTAuthentication(t *testing.T) {
	if (AccountResponse{}).Connected() {
		t.Fatal("empty account connected")
	}
	var apiKeyAccount AccountResponse
	_ = json.Unmarshal([]byte(`{"account":{"type":"apiKey"}}`), &apiKeyAccount)
	if apiKeyAccount.Connected() {
		t.Fatal("API key counted as subscription")
	}
}
