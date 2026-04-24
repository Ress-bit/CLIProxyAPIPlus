package cline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	BaseURL     = "https://api.cline.bot"
	AuthTimeout = 10 * time.Minute
)

type TokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    string `json:"expiresAt"`
	Email        string `json:"email"`
}

type ClineAuth struct {
	client *http.Client
}

func NewClineAuth(cfg *config.Config) *ClineAuth {
	client := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		client = util.SetProxy(&cfg.SDKConfig, client)
	}
	return &ClineAuth{client: client}
}

func (c *ClineAuth) GenerateAuthURL(state, callbackURL string) string {
	return fmt.Sprintf("%s/api/v1/auth/authorize?client_type=extension&callback_url=%s&redirect_uri=%s&state=%s", BaseURL, callbackURL, callbackURL, state)
}

func (c *ClineAuth) ExchangeCode(ctx context.Context, code, redirectURI string) (*TokenResponse, error) {
	payload := map[string]string{
		"grant_type":   "authorization_code",
		"code":         code,
		"redirect_uri": redirectURI,
		"client_type":  "extension",
		"provider":     "workos",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cline: failed to marshal token request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+"/api/v1/auth/token", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("cline: failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Cline/3.0.0")
	req.Header.Set("HTTP-Referer", "https://cline.bot")
	req.Header.Set("X-Title", "Cline")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cline: token request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cline: failed to read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Debugf("cline: token exchange failed (status %d): %s", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("cline: token exchange failed (status %d): %s", resp.StatusCode, string(respBody))
	}
	var tokenResp TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("cline: failed to parse token response: %w", err)
	}
	return &tokenResp, nil
}

func (c *ClineAuth) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	payload := map[string]string{
		"grantType":    "refresh_token",
		"refreshToken": refreshToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cline: failed to marshal refresh request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+"/api/v1/auth/refresh", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("cline: failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Cline/3.0.0")
	req.Header.Set("HTTP-Referer", "https://cline.bot")
	req.Header.Set("X-Title", "Cline")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cline: refresh request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cline: failed to read refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Debugf("cline: token refresh failed (status %d): %s", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("cline: token refresh failed (status %d): %s", resp.StatusCode, string(respBody))
	}
	var tokenResp TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("cline: failed to parse refresh response: %w", err)
	}
	return &tokenResp, nil
}
