package provider

import (
	"context"
	"errors"
	"strings"
	"time"

	"nofx/internal/codexgateway"
	"nofx/mcp"
)

func init() {
	mcp.RegisterProvider(mcp.ProviderCodex, func(opts ...mcp.ClientOption) mcp.AIClient {
		base := mcp.NewClient(append([]mcp.ClientOption{
			mcp.WithProvider(mcp.ProviderCodex), mcp.WithModel(codexgateway.DefaultModel),
		}, opts...)...).(*mcp.Client)
		return &CodexClient{Client: base, timeout: 120 * time.Second}
	})
}

// CodexClient uses a profile reference; the official CLI keeps OAuth tokens
// and their renewal in a separate container, inaccessible to the trading engine.
type CodexClient struct {
	*mcp.Client
	timeout time.Duration
}

func (c *CodexClient) SetAPIKey(profile, _ string, model string) {
	c.APIKey = profile
	if model != "" {
		c.Model = model
	}
}

func (c *CodexClient) SetTimeout(timeout time.Duration) {
	if timeout >= time.Second && timeout <= 120*time.Second {
		c.timeout = timeout
	}
}

func (c *CodexClient) generate(ctx context.Context, system, user, model string) (string, error) {
	gateway, err := codexgateway.New()
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout+3*time.Second)
	defer cancel()
	result, err := gateway.Generate(ctx, c.APIKey, model, system, user, c.timeout)
	if err != nil {
		return "", err
	}
	return result.Content.Response, nil
}

func (c *CodexClient) CallWithMessages(system, user string) (string, error) {
	return c.generate(context.Background(), system, user, c.Model)
}

func (c *CodexClient) CallWithRequest(req *mcp.Request) (string, error) {
	if req == nil {
		return "", errors.New("Codex request is required")
	}
	if len(req.Tools) > 0 {
		return "", errors.New("Codex provider supports text analysis only; tool execution is disabled")
	}
	var system, user strings.Builder
	for _, msg := range req.Messages {
		if len(msg.ToolCalls) > 0 || msg.ToolCallID != "" || msg.Role == "tool" {
			return "", errors.New("Codex provider does not execute tools")
		}
		switch msg.Role {
		case "system", "developer":
			system.WriteString(msg.Content + "\n")
		case "user", "assistant":
			user.WriteString(msg.Role + ":\n" + msg.Content + "\n")
		default:
			return "", errors.New("Unsupported Codex message role")
		}
	}
	model := req.Model
	if model == "" {
		model = c.Model
	}
	return c.generate(req.Ctx, system.String(), user.String(), model)
}

func (c *CodexClient) CallWithRequestFull(req *mcp.Request) (*mcp.LLMResponse, error) {
	content, err := c.CallWithRequest(req)
	if err != nil {
		return nil, err
	}
	return &mcp.LLMResponse{Content: content}, nil
}

func (c *CodexClient) CallWithRequestStream(req *mcp.Request, onChunk func(string)) (string, error) {
	// Publish only a completed, validated answer, never a partial decision.
	content, err := c.CallWithRequest(req)
	if err == nil && onChunk != nil {
		onChunk(content)
	}
	return content, err
}
