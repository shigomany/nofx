// Package codexgateway talks only to the operator-configured, private Codex
// sidecar. Browser requests never choose a profile, URL, or service credential.
package codexgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const DefaultModel = "gpt-5.6-luna"

type Client struct {
	URL   string
	Token string
	HTTP  *http.Client
}

func New() (*Client, error) {
	base := strings.TrimRight(os.Getenv("CODEX_GATEWAY_URL"), "/")
	token := os.Getenv("CODEX_GATEWAY_SERVICE_TOKEN")
	if tokenFile := os.Getenv("CODEX_GATEWAY_SERVICE_TOKEN_FILE"); tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return nil, errors.New("Codex gateway service credential is unavailable")
		}
		token = strings.TrimSpace(string(data))
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(token) < 32 {
		return nil, errors.New("Codex gateway is not configured")
	}
	return &Client{URL: base, Token: token, HTTP: &http.Client{
		Timeout:       125 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// ProfileID is a stable, tenant-scoped reference, not an OAuth token.
func ProfileID(userID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("nofx/codex/"+userID)).String()
}

func (c *Client) Request(ctx context.Context, method, profile, suffix string, payload any, out any) error {
	id, err := uuid.Parse(profile)
	if err != nil || id.String() != profile {
		return errors.New("Invalid Codex profile reference")
	}
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return errors.New("Invalid Codex request")
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+"/v1/profiles/"+profile+"/"+suffix, body)
	if err != nil {
		return errors.New("Invalid Codex gateway configuration")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("Codex gateway request failed or timed out")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Never return upstream bodies: they can contain credentials or prompts.
		return fmt.Errorf("Codex gateway unavailable (status %d)", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(out); err != nil {
		return errors.New("Invalid Codex gateway response")
	}
	return nil
}

type AccountResponse struct {
	Account *struct {
		Type     string `json:"type"`
		PlanType string `json:"planType"`
	} `json:"account"`
	RequiresAuth bool `json:"requiresOpenaiAuth"`
}

func (a AccountResponse) Connected() bool { return a.Account != nil && a.Account.Type == "chatgpt" }

func (c *Client) Account(ctx context.Context, profile string) (AccountResponse, error) {
	var result AccountResponse
	err := c.Request(ctx, http.MethodGet, profile, "account", nil, &result)
	return result, err
}

type Generation struct {
	Content struct {
		Response string `json:"response"`
	} `json:"content"`
	Usage map[string]any `json:"usage"`
}

func (c *Client) Generate(ctx context.Context, profile, model, system, user string, timeout time.Duration) (Generation, error) {
	if strings.TrimSpace(system) == "" {
		system = "Follow the requested response format."
	}
	var result Generation
	err := c.Request(ctx, http.MethodPost, profile, "generate", map[string]any{
		"system": system, "user": user, "model": model, "effort": "low",
		"timeout_seconds": timeout.Seconds(),
		"output_schema": map[string]any{
			"type": "object", "properties": map[string]any{"response": map[string]string{"type": "string"}},
			"required": []string{"response"}, "additionalProperties": false,
		},
	}, &result)
	if err == nil && strings.TrimSpace(result.Content.Response) == "" {
		err = errors.New("Codex returned no final response")
	}
	return result, err
}
