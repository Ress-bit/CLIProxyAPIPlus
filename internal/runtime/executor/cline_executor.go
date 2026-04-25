package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	clineauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/cline"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	clineVersion        = "3.0.0"
	clineBaseURL        = "https://api.cline.bot/api/v1"
	clineModelsEndpoint = "/ai/cline/models"
	clineChatEndpoint   = "/chat/completions"
)

func clineTokenAuthValue(token string) string {
	t := strings.TrimSpace(token)
	if t == "" {
		return ""
	}
	if strings.HasPrefix(t, "workos:") {
		return "Bearer " + t
	}
	return "Bearer workos:" + t
}

type ClineExecutor struct {
	cfg *config.Config
}

func NewClineExecutor(cfg *config.Config) *ClineExecutor { return &ClineExecutor{cfg: cfg} }

func (e *ClineExecutor) Identifier() string { return "cline" }

func clineAccessToken(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		if token, ok := auth.Metadata["accessToken"].(string); ok && token != "" {
			return token
		}
		if token, ok := auth.Metadata["access_token"].(string); ok && token != "" {
			return token
		}
		if token, ok := auth.Metadata["token"].(string); ok && token != "" {
			return token
		}
	}
	if auth.Attributes != nil {
		if token := auth.Attributes["accessToken"]; token != "" {
			return token
		}
		if token := auth.Attributes["access_token"]; token != "" {
			return token
		}
		if token := auth.Attributes["token"]; token != "" {
			return token
		}
	}
	return ""
}

func (e *ClineExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	accessToken, err := e.ensureFreshAccessToken(req.Context(), auth)
	if err != nil {
		return err
	}
	if strings.TrimSpace(accessToken) == "" {
		return statusErr{code: http.StatusUnauthorized, msg: "cline: missing access token"}
	}
	req.Header.Set("Authorization", clineTokenAuthValue(accessToken))
	if auth != nil {
		util.ApplyCustomHeadersFromAttrs(req, auth.Attributes)
	}
	return nil
}

func (e *ClineExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("cline executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}
func (e *ClineExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	baseModel := req.Model
	from := opts.SourceFormat
	to := sdktranslator.FromString("openai")
	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayloadSource, false)
	translated := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, false)
	requestedModel := payloadRequestedModel(opts, req.Model)
	translated = applyPayloadConfigWithRoot(e.cfg, baseModel, to.String(), "", translated, originalTranslated, requestedModel)
	translated = applyClineOpenRouterParity(translated, false)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, clineBaseURLFromAuth(auth)+clineChatEndpoint, bytes.NewReader(translated))
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	accessToken := clineAccessToken(auth)
	applyClineHeaders(httpReq, accessToken, false)
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(httpReq, attrs)
	httpResp, err := e.HttpRequest(ctx, auth, httpReq)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		return cliproxyexecutor.Response{}, statusErr{code: httpResp.StatusCode, msg: string(b)}
	}
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	return cliproxyexecutor.Response{Payload: body, Headers: httpResp.Header.Clone()}, nil
}
func (e *ClineExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	baseModel := req.Model
	from := opts.SourceFormat
	to := sdktranslator.FromString("openai")
	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayloadSource, true)
	translated := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, true)
	requestedModel := payloadRequestedModel(opts, req.Model)
	translated = applyPayloadConfigWithRoot(e.cfg, baseModel, to.String(), "", translated, originalTranslated, requestedModel)
	translated = applyClineOpenRouterParity(translated, true)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, clineBaseURLFromAuth(auth)+clineChatEndpoint, bytes.NewReader(translated))
	if err != nil {
		return nil, err
	}
	accessToken := clineAccessToken(auth)
	applyClineHeaders(httpReq, accessToken, true)
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(httpReq, attrs)
	httpResp, err := e.HttpRequest(ctx, auth, httpReq)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		defer httpResp.Body.Close()
		b, _ := io.ReadAll(httpResp.Body)
		return nil, statusErr{code: httpResp.StatusCode, msg: string(b)}
	}
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer httpResp.Body.Close()
		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 52_428_800)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			out <- cliproxyexecutor.StreamChunk{Payload: bytes.Clone(line)}
		}
		if errScan := scanner.Err(); errScan != nil {
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, nil
}
func (e *ClineExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}
func (e *ClineExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func clineRefreshToken(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		if token, ok := auth.Metadata["refreshToken"].(string); ok && strings.TrimSpace(token) != "" {
			return strings.TrimSpace(token)
		}
		if token, ok := auth.Metadata["refresh_token"].(string); ok && strings.TrimSpace(token) != "" {
			return strings.TrimSpace(token)
		}
	}
	if auth.Attributes != nil {
		if token := strings.TrimSpace(auth.Attributes["refreshToken"]); token != "" {
			return token
		}
		if token := strings.TrimSpace(auth.Attributes["refresh_token"]); token != "" {
			return token
		}
	}
	return ""
}

func (e *ClineExecutor) ensureFreshAccessToken(ctx context.Context, auth *cliproxyauth.Auth) (string, error) {
	accessToken := clineAccessToken(auth)
	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("cline: missing access token")
	}
	refreshToken := clineRefreshToken(auth)
	if refreshToken == "" {
		return accessToken, nil
	}
	authSvc := clineauth.NewClineAuth(e.cfg)
	refreshed, err := authSvc.RefreshToken(ctx, refreshToken)
	if err != nil {
		log.Warnf("cline: token refresh failed, fallback to current token: %v", err)
		return accessToken, nil
	}
	if refreshed == nil || strings.TrimSpace(refreshed.AccessToken) == "" {
		return accessToken, nil
	}
	newAccessToken := strings.TrimSpace(refreshed.AccessToken)
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["accessToken"] = newAccessToken
	auth.Metadata["access_token"] = newAccessToken
	if strings.TrimSpace(refreshed.RefreshToken) != "" {
		newRefresh := strings.TrimSpace(refreshed.RefreshToken)
		auth.Metadata["refreshToken"] = newRefresh
		auth.Metadata["refresh_token"] = newRefresh
	}
	if strings.TrimSpace(refreshed.ExpiresAt) != "" {
		if t, parseErr := time.Parse(time.RFC3339Nano, refreshed.ExpiresAt); parseErr == nil {
			auth.Metadata["expiresAt"] = t.Unix()
			auth.Metadata["expires_at"] = t.Format(time.RFC3339)
		} else if t, parseErr2 := time.Parse(time.RFC3339, refreshed.ExpiresAt); parseErr2 == nil {
			auth.Metadata["expiresAt"] = t.Unix()
			auth.Metadata["expires_at"] = t.Format(time.RFC3339)
		}
	}
	return newAccessToken, nil
}

func applyClineHeaders(r *http.Request, token string, stream bool) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", clineTokenAuthValue(token))
	r.Header.Set("HTTP-Referer", "https://cline.bot")
	r.Header.Set("X-Title", "Cline")
	r.Header.Set("X-Task-ID", "")
	r.Header.Set("X-CLIENT-TYPE", "cli")
	r.Header.Set("X-CORE-VERSION", clineVersion)
	r.Header.Set("X-IS-MULTIROOT", "false")
	r.Header.Set("X-CLIENT-VERSION", clineVersion)
	r.Header.Set("X-PLATFORM", runtime.GOOS)
	r.Header.Set("X-PLATFORM-VERSION", runtime.Version())
	r.Header.Set("User-Agent", "Cline/"+clineVersion)
	if stream {
		r.Header.Set("Accept", "text/event-stream")
		r.Header.Set("Cache-Control", "no-cache")
	} else {
		r.Header.Set("Accept", "application/json")
	}
}

func applyClineOpenRouterParity(payload []byte, stream bool) []byte {
	if len(payload) == 0 {
		return payload
	}
	out := payload
	if stream {
		if updated, err := sjson.SetRawBytes(out, "stream_options", []byte(`{"include_usage":true}`)); err == nil {
			out = updated
		}
		if updated, err := sjson.SetBytes(out, "include_reasoning", true); err == nil {
			out = updated
		}
	} else {
		if updated, err := sjson.DeleteBytes(out, "stream_options"); err == nil {
			out = updated
		}
		if updated, err := sjson.SetBytes(out, "include_reasoning", true); err == nil {
			out = updated
		}
	}
	modelID := strings.TrimSpace(gjson.GetBytes(out, "model").String())
	if modelID == "" {
		return out
	}
	if strings.Contains(modelID, "kwaipilot/kat-coder-pro") {
		trimmedModel := strings.TrimSuffix(modelID, ":free")
		if updated, err := sjson.SetBytes(out, "model", trimmedModel); err == nil {
			out = updated
		}
		if updated, err := sjson.SetRawBytes(out, "provider", []byte(`{"sort":"throughput"}`)); err == nil {
			out = updated
		}
	}
	return out
}

type ClineModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MaxTokens   int    `json:"max_tokens"`
	ContextLen  int    `json:"context_length"`
	Pricing     struct {
		Prompt         string `json:"prompt"`
		Completion     string `json:"completion"`
		InputCacheRead string `json:"input_cache_read"`
		WebSearch      string `json:"web_search"`
	} `json:"pricing"`
}

func clineBaseURLFromAuth(auth *cliproxyauth.Auth) string {
	if auth != nil && auth.Metadata != nil {
		if baseURL, ok := auth.Metadata["base_url"].(string); ok && strings.TrimSpace(baseURL) != "" {
			return strings.TrimRight(strings.TrimSpace(baseURL), "/")
		}
	}
	return clineBaseURL
}

func clineIsFreeModel(m ClineModel) bool {
	promptRaw := strings.TrimSpace(m.Pricing.Prompt)
	completionRaw := strings.TrimSpace(m.Pricing.Completion)
	if promptRaw == "" || completionRaw == "" {
		return false
	}
	promptPrice, errPrompt := strconv.ParseFloat(promptRaw, 64)
	completionPrice, errCompletion := strconv.ParseFloat(completionRaw, 64)
	if errPrompt != nil || errCompletion != nil {
		return false
	}
	return promptPrice == 0 && completionPrice == 0
}

func FetchClineModels(ctx context.Context, auth *cliproxyauth.Auth, cfg *config.Config) []*registry.ModelInfo {
	return registry.GetClineModels()
}
