package provider

import (
	"fmt"
	"io"
	"net/http"

	"nofx/mcp"
)

const (
	// DefaultZAIBaseURL is the OpenAI-compatible Z.ai API endpoint.
	DefaultZAIBaseURL = "https://api.z.ai/api/paas/v4"
	DefaultZAIModel   = "glm-5.3"

	zaiProviderName = mcp.ProviderZAI
)

func init() {
	mcp.RegisterProvider(zaiProviderName, func(opts ...mcp.ClientOption) mcp.AIClient {
		return NewZAIClientWithOptions(opts...)
	})
}

// ZAIClient is a client for Z.ai's standard OpenAI-compatible API.
//
// Z.ai's Coding Plan endpoint is intentionally not supported here. This
// provider only uses the standard PaaS chat-completions API.
type ZAIClient struct {
	*mcp.Client
}

func (c *ZAIClient) BaseClient() *mcp.Client { return c.Client }

// NewZAIClient creates a Z.ai client with the standard defaults.
func NewZAIClient() mcp.AIClient {
	return NewZAIClientWithOptions()
}

// NewZAIClientWithOptions creates a Z.ai client with optional overrides.
func NewZAIClientWithOptions(opts ...mcp.ClientOption) mcp.AIClient {
	baseClient := mcp.NewClient(append([]mcp.ClientOption{
		mcp.WithProvider(zaiProviderName),
		mcp.WithModel(DefaultZAIModel),
		mcp.WithBaseURL(DefaultZAIBaseURL),
	}, opts...)...).(*mcp.Client)

	c := &ZAIClient{Client: baseClient}
	baseClient.Hooks = c
	return c
}

// SetAPIKey stores the API key and optional endpoint/model overrides. API keys
// are deliberately never written to logs, including partial redactions.
func (c *ZAIClient) SetAPIKey(apiKey, customURL, customModel string) {
	c.APIKey = apiKey
	if customURL != "" {
		c.BaseURL = customURL
		c.Log.Infof("🔧 [MCP] Z.ai using custom BaseURL: %s", customURL)
	}
	if customModel != "" {
		c.Model = customModel
		c.Log.Infof("🔧 [MCP] Z.ai using custom Model: %s", customModel)
	}
}

// SetAuthHeader uses the standard OpenAI-compatible Bearer authentication.
func (c *ZAIClient) SetAuthHeader(headers http.Header) {
	c.Client.SetAuthHeader(headers)
}

// Call mirrors the shared legacy call path without the base client's API-key
// debug log. This keeps the Z.ai provider from emitting any part of a key.
func (c *ZAIClient) Call(systemPrompt, userPrompt string) (string, error) {
	requestBody := c.BuildMCPRequestBody(systemPrompt, userPrompt)
	jsonData, err := c.MarshalRequestBody(requestBody)
	if err != nil {
		return "", err
	}
	req, err := c.BuildRequest(c.BuildUrl(), jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Upstream error text can echo request data or credentials.
		return "", fmt.Errorf("Z.ai API returned error (status %d)", resp.StatusCode)
	}
	result, err := c.ParseMCPResponse(body)
	if err != nil {
		return "", fmt.Errorf("fail to parse AI server response: %w", err)
	}
	return result, nil
}

// BuildMCPRequestBody disables GLM thinking by default. The base builder
// retains the normal max-token and message handling for the legacy path.
func (c *ZAIClient) BuildMCPRequestBody(systemPrompt, userPrompt string) map[string]any {
	body := c.Client.BuildMCPRequestBody(systemPrompt, userPrompt)
	body["thinking"] = map[string]string{"type": "disabled"}
	return body
}

// BuildRequestBodyFromRequest preserves tools, streaming, and all optional
// request fields from the shared builder while disabling GLM thinking.
func (c *ZAIClient) BuildRequestBodyFromRequest(req *mcp.Request) map[string]any {
	body := c.Client.BuildRequestBodyFromRequest(req)
	body["thinking"] = map[string]string{"type": "disabled"}
	return body
}
