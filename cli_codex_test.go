package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nofx/internal/codexgateway"
	"nofx/store"
)

func TestAccountResetRevokesCodexBeforeUserIDsAreRemoved(t *testing.T) {
	st, err := store.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.User().Create(&store.User{ID: "alice", Email: "alice@example.test", PasswordHash: "test-only"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	status := http.StatusOK
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/profiles/"+codexgateway.ProfileID("alice")+"/logout" {
			t.Errorf("unexpected profile: %s", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer gateway.Close()
	t.Setenv("CODEX_GATEWAY_URL", gateway.URL)
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN", strings.Repeat("x", 32))
	t.Setenv("CODEX_GATEWAY_SERVICE_TOKEN_FILE", "")
	if err := disconnectCodexForAccountReset(st); err != nil || calls != 1 {
		t.Fatalf("logout failed: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := disconnectCodexForAccountReset(st); err == nil {
		t.Fatal("failed logout was accepted")
	}
	if _, err := st.User().GetByID("alice"); err != nil {
		t.Fatal("user ID was lost before session revocation")
	}
}
