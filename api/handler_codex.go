package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nofx/internal/codexgateway"

	"github.com/gin-gonic/gin"
)

func codexService(c *gin.Context) (*codexgateway.Client, string, bool) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return nil, "", false
	}
	gateway, err := codexgateway.New()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return nil, "", false
	}
	return gateway, codexgateway.ProfileID(userID), true
}

func (s *Server) handleCodexConnect(c *gin.Context) {
	gateway, profile, ok := codexService(c)
	if !ok {
		return
	}
	var result struct {
		LoginID string `json:"loginId"`
		URL     string `json:"verificationUrl"`
		Code    string `json:"userCode"`
	}
	if err := gateway.Request(c.Request.Context(), http.MethodPost, profile, "login/start", nil, &result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	u, err := url.Parse(result.URL)
	if err != nil || u.Scheme != "https" || u.Host != "auth.openai.com" || u.User != nil || result.Code == "" || result.LoginID == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Invalid Codex sign-in response"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"loginId": result.LoginID, "verificationUrl": result.URL, "userCode": result.Code})
}

func (s *Server) handleCodexStatus(c *gin.Context) {
	gateway, profile, ok := codexService(c)
	if !ok {
		return
	}
	status := "unknown"
	loginID := c.Query("login_id")
	if len(loginID) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid login ID"})
		return
	}
	if loginID != "" {
		var result struct {
			Status string `json:"status"`
		}
		if err := gateway.Request(c.Request.Context(), http.MethodGet, profile, "login/status?login_id="+url.QueryEscape(loginID), nil, &result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		switch result.Status {
		case "pending", "connected", "failed", "cancelled":
			status = result.Status
		}
	}
	account, err := gateway.Account(c.Request.Context(), profile)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	connected := account.Connected() && (loginID == "" || status == "connected")
	plan := ""
	if connected {
		status = "connected"
		plan = account.Account.PlanType
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"connected": connected, "status": status, "planType": plan})
}

func (s *Server) handleCodexModels(c *gin.Context) {
	gateway, profile, ok := codexService(c)
	if !ok {
		return
	}
	account, err := gateway.Account(c.Request.Context(), profile)
	if err != nil || !account.Connected() {
		c.JSON(http.StatusConflict, gin.H{"error": "Connect your ChatGPT account first"})
		return
	}
	result, err := gateway.Models(c.Request.Context(), profile)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleCodexCancel(c *gin.Context) {
	gateway, profile, ok := codexService(c)
	if !ok {
		return
	}
	var req struct {
		LoginID string `json:"login_id"`
	}
	if c.ShouldBindJSON(&req) != nil || req.LoginID == "" || len(req.LoginID) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Login ID required"})
		return
	}
	if err := gateway.Request(c.Request.Context(), http.MethodPost, profile, "login/cancel", req, nil); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cancelled": true})
}

func (s *Server) handleCodexTest(c *gin.Context) {
	gateway, profile, ok := codexService(c)
	if !ok {
		return
	}
	var req struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Model) == "" || len(req.Model) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model required"})
		return
	}
	if err := codexgateway.ValidateEffort(req.Model, req.Effort); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	account, err := gateway.Account(c.Request.Context(), profile)
	if err != nil || !account.Connected() {
		c.JSON(http.StatusConflict, gin.H{"error": "Connect your ChatGPT account first"})
		return
	}
	if err := gateway.ValidateSelection(c.Request.Context(), profile, req.Model, req.Effort); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 123*time.Second)
	defer cancel()
	result, err := gateway.Generate(ctx, profile, req.Model, "Return the requested text in the response field.", "Reply with exactly OK.", 120*time.Second, req.Effort)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "response": result.Content.Response})
}

func (s *Server) handleCodexDisconnect(c *gin.Context) {
	gateway, profile, ok := codexService(c)
	if !ok {
		return
	}
	userID := c.GetString("user_id")
	models, err := s.store.AIModel().List(userID)
	if err != nil {
		SafeInternalError(c, "Read Codex configurations", err)
		return
	}
	for _, model := range models {
		if model.Provider != "codex" {
			continue
		}
		traders, err := s.store.Trader().ListByAIModelID(userID, model.ID)
		if err != nil {
			SafeInternalError(c, "Read Codex traders", err)
			return
		}
		for _, trader := range traders {
			if trader.IsRunning {
				c.JSON(http.StatusConflict, gin.H{"error": "Stop your Codex traders before disconnecting the account"})
				return
			}
		}
	}
	// Logout happens only through this explicit user action; modal closure only
	// cancels a pending login and never removes a saved account session.
	if err := gateway.Request(c.Request.Context(), http.MethodPost, profile, "logout", nil, nil); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	for _, model := range models {
		if model.Provider == "codex" {
			if err := s.store.AIModel().Update(userID, model.ID, false, "", "", model.CustomModelName); err != nil {
				SafeInternalError(c, "Disable Codex configuration", err)
				return
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"disconnected": true})
}
