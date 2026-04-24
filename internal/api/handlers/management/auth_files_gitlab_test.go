package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	clineauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/cline"
	codebuddyauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codebuddy"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func assertJSONShape(t *testing.T, payload map[string]any, keys ...string) {
	t.Helper()

	if len(payload) != len(keys) {
		t.Fatalf("response keys = %v, want exactly %v", mapKeys(payload), keys)
	}

	for _, key := range keys {
		if _, ok := payload[key]; !ok {
			t.Fatalf("response missing key %q; keys = %v", key, mapKeys(payload))
		}
	}
}

func mapKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

type fakeCodeBuddyService struct {
	fetchState *codebuddyauth.AuthState
	fetchErr   error
	pollResult *codebuddyauth.CodeBuddyTokenStorage
	pollErr    error
	pollState  string
	pollMu     sync.Mutex
	pollCalls  atomic.Int32
	pollBlock  chan struct{}
}

func (f *fakeCodeBuddyService) FetchAuthState(context.Context) (*codebuddyauth.AuthState, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	if f.fetchState == nil {
		return nil, nil
	}
	clone := *f.fetchState
	return &clone, nil
}

func (f *fakeCodeBuddyService) PollForToken(_ context.Context, state string) (*codebuddyauth.CodeBuddyTokenStorage, error) {
	f.pollCalls.Add(1)
	f.pollMu.Lock()
	f.pollState = state
	f.pollMu.Unlock()
	if f.pollBlock != nil {
		<-f.pollBlock
	}
	if f.pollErr != nil {
		return nil, f.pollErr
	}
	if f.pollResult == nil {
		return nil, nil
	}
	clone := *f.pollResult
	return &clone, nil
}

func (f *fakeCodeBuddyService) recordedPollState() string {
	f.pollMu.Lock()
	defer f.pollMu.Unlock()
	return f.pollState
}

type fakeClineAuthService struct {
	authURL      string
	exchange     *clineauth.TokenResponse
	exchangeErr  error
	lastCode     string
	lastRedirect string
	mu           sync.Mutex
}

func (f *fakeClineAuthService) GenerateAuthURL(state, callbackURL string) string {
	if f.authURL != "" {
		return f.authURL
	}
	return "https://cline.example.com/auth?state=" + state + "&callback=" + callbackURL
}

func (f *fakeClineAuthService) ExchangeCode(_ context.Context, code, redirectURI string) (*clineauth.TokenResponse, error) {
	f.mu.Lock()
	f.lastCode = code
	f.lastRedirect = redirectURI
	f.mu.Unlock()
	if f.exchangeErr != nil {
		return nil, f.exchangeErr
	}
	return f.exchange, nil
}

func (f *fakeClineAuthService) recordedCode() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCode
}

func (f *fakeClineAuthService) recordedRedirect() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastRedirect
}

func TestRequestGitLabPATToken_SavesAuthRecord(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer glpat-test-token" {
			t.Fatalf("authorization header = %q, want Bearer glpat-test-token", got)
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       42,
				"username": "gitlab-user",
				"name":     "GitLab User",
				"email":    "gitlab@example.com",
			})
		case "/api/v4/personal_access_tokens/self":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      7,
				"name":    "management-center",
				"scopes":  []string{"api", "read_user"},
				"user_id": 42,
			})
		case "/api/v4/code_suggestions/direct_access":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"base_url":   "https://cloud.gitlab.example.com",
				"token":      "gateway-token",
				"expires_at": 1893456000,
				"headers": map[string]string{
					"X-Gitlab-Realm": "saas",
				},
				"model_details": map[string]any{
					"model_provider": "anthropic",
					"model_name":     "claude-sonnet-4-5",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	store := &memoryAuthStore{}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, coreauth.NewManager(nil, nil, nil))
	h.tokenStore = store

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/gitlab-auth-url", strings.NewReader(`{"base_url":"`+upstream.URL+`","personal_access_token":"glpat-test-token"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.RequestGitLabPATToken(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := resp["status"]; got != "ok" {
		t.Fatalf("status = %#v, want ok", got)
	}
	if got := resp["model_provider"]; got != "anthropic" {
		t.Fatalf("model_provider = %#v, want anthropic", got)
	}
	if got := resp["model_name"]; got != "claude-sonnet-4-5" {
		t.Fatalf("model_name = %#v, want claude-sonnet-4-5", got)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.items) != 1 {
		t.Fatalf("expected 1 saved auth record, got %d", len(store.items))
	}
	var saved *coreauth.Auth
	for _, item := range store.items {
		saved = item
	}
	if saved == nil {
		t.Fatal("expected saved auth record")
	}
	if saved.Provider != "gitlab" {
		t.Fatalf("provider = %q, want gitlab", saved.Provider)
	}
	if got := saved.Metadata["auth_kind"]; got != "personal_access_token" {
		t.Fatalf("auth_kind = %#v, want personal_access_token", got)
	}
	if got := saved.Metadata["model_provider"]; got != "anthropic" {
		t.Fatalf("saved model_provider = %#v, want anthropic", got)
	}
	if got := saved.Metadata["duo_gateway_token"]; got != "gateway-token" {
		t.Fatalf("saved duo_gateway_token = %#v, want gateway-token", got)
	}
}

func TestPostOAuthCallback_GitLabWritesPendingCallbackFile(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	state := "gitlab-state-123"
	RegisterOAuthSession(state, "gitlab")
	t.Cleanup(func() { CompleteOAuthSession(state) })

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, coreauth.NewManager(nil, nil, nil))

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/oauth-callback", strings.NewReader(`{"provider":"gitlab","redirect_url":"http://localhost:17171/auth/callback?code=test-code&state=`+state+`"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PostOAuthCallback(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	filePath := filepath.Join(authDir, ".oauth-gitlab-"+state+".oauth")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read callback file: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode callback payload: %v", err)
	}
	if got := payload["code"]; got != "test-code" {
		t.Fatalf("callback code = %q, want test-code", got)
	}
	if got := payload["state"]; got != state {
		t.Fatalf("callback state = %q, want %q", got, state)
	}
}

func TestNormalizeOAuthProvider_GitLab(t *testing.T) {
	provider, err := NormalizeOAuthProvider("gitlab")
	if err != nil {
		t.Fatalf("NormalizeOAuthProvider returned error: %v", err)
	}
	if provider != "gitlab" {
		t.Fatalf("provider = %q, want gitlab", provider)
	}
}

func TestNormalizeOAuthProvider_CodeBuddy(t *testing.T) {
	provider, err := NormalizeOAuthProvider("codebuddy")
	if err != nil {
		t.Fatalf("NormalizeOAuthProvider returned error: %v", err)
	}
	if provider != "codebuddy" {
		t.Fatalf("provider = %q, want codebuddy", provider)
	}
}

func TestNormalizeOAuthProvider_CodeBuddyIntl(t *testing.T) {
	provider, err := NormalizeOAuthProvider("codebuddy-intl")
	if err != nil {
		t.Fatalf("NormalizeOAuthProvider returned error: %v", err)
	}
	if provider != "codebuddy-intl" {
		t.Fatalf("provider = %q, want codebuddy-intl", provider)
	}
}

func TestRequestCodeBuddyToken_SavesAuthRecord(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	store := &memoryAuthStore{}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, coreauth.NewManager(nil, nil, nil))
	h.tokenStore = store

	prevFactory := newCodeBuddyAuthService
	fake := &fakeCodeBuddyService{
		// Keep the login URL fixture neutral while the saved token domain matches
		// the external CodeBuddy environment defaults.
		fetchState: &codebuddyauth.AuthState{State: "remote-state-123", AuthURL: "https://codebuddy.example.com/login"},
		pollBlock:  make(chan struct{}),
		pollResult: &codebuddyauth.CodeBuddyTokenStorage{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresIn:    7200,
			TokenType:    "Bearer",
			Domain:       "www.codebuddy.ai",
			UserID:       "user-123",
			Type:         "codebuddy",
		},
	}
	newCodeBuddyAuthService = func(*config.Config) codeBuddyAuthService { return fake }
	t.Cleanup(func() { newCodeBuddyAuthService = prevFactory })

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/codebuddy-auth-url", nil)

	h.RequestCodeBuddyToken(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertJSONShape(t, resp, "state", "url")
	if got := resp["url"]; got != "https://codebuddy.example.com/login" {
		t.Fatalf("url = %#v, want https://codebuddy.example.com/login", got)
	}
	state, _ := resp["state"].(string)
	if !strings.HasPrefix(state, "codebuddy-") {
		t.Fatalf("state = %q, want prefix codebuddy-", state)
	}

	provider, status, ok := GetOAuthSession(state)
	if !ok {
		t.Fatal("expected OAuth session to be registered")
	}
	if provider != "codebuddy" {
		t.Fatalf("provider = %q, want codebuddy", provider)
	}
	if status != "" {
		t.Fatalf("status = %q, want empty pending status", status)
	}

	close(fake.pollBlock)

	requireEventually(t, func() bool {
		_, currentStatus, exists := GetOAuthSession(state)
		return exists && currentStatus == oauthSessionSuccess
	})
	_, successStatus, ok := GetOAuthSession(state)
	if !ok {
		t.Fatal("expected OAuth session to remain registered after success")
	}
	if successStatus != oauthSessionSuccess {
		t.Fatalf("success status = %q, want %q", successStatus, oauthSessionSuccess)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.items) != 1 {
		t.Fatalf("expected 1 saved auth record, got %d", len(store.items))
	}
	var saved *coreauth.Auth
	for _, item := range store.items {
		saved = item
	}
	if saved == nil {
		t.Fatal("expected saved auth record")
	}
	if saved.Provider != "codebuddy" {
		t.Fatalf("provider = %q, want codebuddy", saved.Provider)
	}
	if saved.FileName != "codebuddy-user-123.json" {
		t.Fatalf("file name = %q, want codebuddy-user-123.json", saved.FileName)
	}
	if got := fake.pollCalls.Load(); got != 1 {
		t.Fatalf("poll calls = %d, want 1", got)
	}
	if got := fake.recordedPollState(); got != "remote-state-123" {
		t.Fatalf("polled state = %q, want remote-state-123", got)
	}
}

func TestRequestCodeBuddyToken_PollingNotSupported(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, coreauth.NewManager(nil, nil, nil))
	prevFactory := newCodeBuddyIntlAuthService
	newCodeBuddyAuthService = func(*config.Config) codeBuddyAuthService {
		return &fakeCodeBuddyService{fetchState: &codebuddyauth.AuthState{State: "", AuthURL: "https://codebuddy.example.com/login"}}
	}
	t.Cleanup(func() { newCodeBuddyAuthService = prevFactory })

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/codebuddy-auth-url", nil)

	h.RequestCodeBuddyToken(ctx)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusConflict, rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := resp["error"]; got != "codebuddy_polling_not_supported" {
		t.Fatalf("error = %#v, want codebuddy_polling_not_supported", got)
	}
}

func TestGetAuthStatus_CodeBuddyWaitOkAndErrorShapes(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, coreauth.NewManager(nil, nil, nil))

	t.Run("ok without state", func(t *testing.T) {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/get-auth-status", nil)
		h.GetAuthStatus(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("ok status code = %d, want %d", rec.Code, http.StatusOK)
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode ok response: %v", err)
		}
		assertJSONShape(t, resp, "status")
		if got := resp["status"]; got != "ok" {
			t.Fatalf("ok response status = %#v, want ok", got)
		}
	})

	t.Run("wait with pending codebuddy state", func(t *testing.T) {
		state := "codebuddy-test-state-wait"
		RegisterOAuthSession(state, "codebuddy")
		t.Cleanup(func() { CompleteOAuthSession(state) })

		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/get-auth-status?state="+state, nil)
		h.GetAuthStatus(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("wait status code = %d, want %d", rec.Code, http.StatusOK)
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode wait response: %v", err)
		}
		assertJSONShape(t, resp, "status")
		if got := resp["status"]; got != "wait" {
			t.Fatalf("wait response status = %#v, want wait", got)
		}
	})

	t.Run("error for unknown codebuddy state", func(t *testing.T) {
		state := "codebuddy-test-state-error"

		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/get-auth-status?state="+state, nil)
		h.GetAuthStatus(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("unknown status code = %d, want %d", rec.Code, http.StatusOK)
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode unknown response: %v", err)
		}
		assertJSONShape(t, resp, "error", "status")
		if got := resp["status"]; got != "error" {
			t.Fatalf("unknown response status = %#v, want error", got)
		}
		if _, ok := resp["error"].(string); !ok {
			t.Fatalf("unknown response error = %#v, want string", resp["error"])
		}
	})

	t.Run("ok after successful codebuddy completion", func(t *testing.T) {
		state := "codebuddy-test-state-success"
		RegisterOAuthSession(state, "codebuddy")
		SetOAuthSessionSuccess(state)

		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/get-auth-status?state="+state, nil)
		h.GetAuthStatus(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("success status code = %d, want %d", rec.Code, http.StatusOK)
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode success response: %v", err)
		}
		assertJSONShape(t, resp, "status")
		if got := resp["status"]; got != "ok" {
			t.Fatalf("success response status = %#v, want ok", got)
		}
	})

	t.Run("ok after successful codebuddy completion even after provider cleanup", func(t *testing.T) {
		state := "codebuddy-test-state-success-cleanup"
		RegisterOAuthSession(state, "codebuddy")
		SetOAuthSessionSuccess(state)
		CompleteOAuthSessionsByProvider("codebuddy")

		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/get-auth-status?state="+state, nil)
		h.GetAuthStatus(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("success cleanup status code = %d, want %d", rec.Code, http.StatusOK)
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode success cleanup response: %v", err)
		}
		assertJSONShape(t, resp, "status")
		if got := resp["status"]; got != "ok" {
			t.Fatalf("success cleanup response status = %#v, want ok", got)
		}
	})
}

func TestRequestCodeBuddyToken_PollFailureMarksSessionError(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, coreauth.NewManager(nil, nil, nil))
	prevFactory := newCodeBuddyAuthService
	newCodeBuddyAuthService = func(*config.Config) codeBuddyAuthService {
		return &fakeCodeBuddyService{
			fetchState: &codebuddyauth.AuthState{State: "remote-state-err", AuthURL: "https://codebuddy.example.com/login"},
			pollErr:    errors.New("poll failed"),
		}
	}
	t.Cleanup(func() { newCodeBuddyAuthService = prevFactory })

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/codebuddy-auth-url", nil)

	h.RequestCodeBuddyToken(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	state, _ := resp["state"].(string)
	requireEventually(t, func() bool {
		_, status, ok := GetOAuthSession(state)
		return ok && status != ""
	})
	_, status, ok := GetOAuthSession(state)
	if !ok {
		t.Fatal("expected OAuth session to remain with error status")
	}
	if status == "" {
		t.Fatal("expected OAuth session error status to be populated")
	}
}

func TestRequestCodeBuddyIntlToken_SavesAuthRecord(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	store := &memoryAuthStore{}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, coreauth.NewManager(nil, nil, nil))
	h.tokenStore = store

	prevFactory := newCodeBuddyAuthService
	fake := &fakeCodeBuddyService{
		fetchState: &codebuddyauth.AuthState{State: "remote-state-intl-123", AuthURL: "https://www.codebuddy.ai/login"},
		pollBlock:  make(chan struct{}),
		pollResult: &codebuddyauth.CodeBuddyTokenStorage{
			AccessToken:  "intl-access-token",
			RefreshToken: "intl-refresh-token",
			ExpiresIn:    7200,
			TokenType:    "Bearer",
			Domain:       codebuddyauth.IntlDefaultDomain,
			UserID:       "intl-user-123",
			Email:        "intl@example.com",
			Type:         "codebuddy-intl",
		},
	}
	newCodeBuddyIntlAuthService = func(*config.Config) codeBuddyAuthService { return fake }
	t.Cleanup(func() { newCodeBuddyIntlAuthService = prevFactory })

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/codebuddy-intl-auth-url", nil)

	h.RequestCodeBuddyIntlToken(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertJSONShape(t, resp, "state", "url")
	if got := resp["url"]; got != "https://www.codebuddy.ai/login" {
		t.Fatalf("url = %#v, want https://www.codebuddy.ai/login", got)
	}
	state, _ := resp["state"].(string)
	if !strings.HasPrefix(state, "codebuddy-intl-") {
		t.Fatalf("state = %q, want prefix codebuddy-intl-", state)
	}

	provider, status, ok := GetOAuthSession(state)
	if !ok {
		t.Fatal("expected OAuth session to be registered")
	}
	if provider != "codebuddy-intl" {
		t.Fatalf("provider = %q, want codebuddy-intl", provider)
	}
	if status != "" {
		t.Fatalf("status = %q, want empty pending status", status)
	}

	close(fake.pollBlock)

	requireEventually(t, func() bool {
		_, currentStatus, exists := GetOAuthSession(state)
		return exists && currentStatus == oauthSessionSuccess
	})

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.items) != 1 {
		t.Fatalf("expected 1 saved auth record, got %d", len(store.items))
	}
	var saved *coreauth.Auth
	for _, item := range store.items {
		saved = item
	}
	if saved == nil {
		t.Fatal("expected saved auth record")
	}
	if saved.Provider != "codebuddy-intl" {
		t.Fatalf("provider = %q, want codebuddy-intl", saved.Provider)
	}
	if saved.FileName != "codebuddy-intl-intl-user-123.json" {
		t.Fatalf("file name = %q, want codebuddy-intl-intl-user-123.json", saved.FileName)
	}
	if got := saved.Label; got != "intl@example.com" {
		t.Fatalf("label = %q, want intl@example.com", got)
	}
	if got := saved.Metadata["base_url"]; got != codebuddyauth.IntlBaseURL {
		t.Fatalf("base_url = %#v, want %q", got, codebuddyauth.IntlBaseURL)
	}
	if got := fake.pollCalls.Load(); got != 1 {
		t.Fatalf("poll calls = %d, want 1", got)
	}
	if got := fake.recordedPollState(); got != "remote-state-intl-123" {
		t.Fatalf("polled state = %q, want remote-state-intl-123", got)
	}
}

func TestRequestClineToken_SavesAuthRecord(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	store := &memoryAuthStore{}
	authDir := t.TempDir()
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, coreauth.NewManager(nil, nil, nil))
	h.tokenStore = store

	prevFactory := newClineAuthService
	fake := &fakeClineAuthService{
		authURL: "https://cline.example.com/auth",
		exchange: &clineauth.TokenResponse{
			AccessToken:  "cline-access-token",
			RefreshToken: "cline-refresh-token",
			ExpiresAt:    "2026-04-24T12:34:56Z",
			Email:        "cline@example.com",
		},
	}
	newClineAuthService = func(*config.Config) clineAuthService { return fake }
	t.Cleanup(func() { newClineAuthService = prevFactory })

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	body := strings.NewReader(`{"callback_url":"http://localhost:1455/callback"}`)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/request-cline-token", body)
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.RequestClineToken(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertJSONShape(t, resp, "status", "url", "state")
	if got := resp["status"]; got != "ok" {
		t.Fatalf("status = %#v, want ok", got)
	}
	state, _ := resp["state"].(string)
	if state == "" {
		t.Fatal("expected non-empty state")
	}

	provider, status, ok := GetOAuthSession(state)
	if !ok {
		t.Fatal("expected OAuth session to be registered")
	}
	if provider != "cline" {
		t.Fatalf("provider = %q, want cline", provider)
	}
	if status != "" {
		t.Fatalf("status = %q, want empty pending status", status)
	}

	callbackFile := filepath.Join(authDir, ".oauth-cline-"+state+".oauth")
	payload := `{"code":"auth-code-123","state":"` + state + `"}`
	if err := os.WriteFile(callbackFile, []byte(payload), 0o600); err != nil {
		t.Fatalf("write callback file: %v", err)
	}

	requireEventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return len(store.items) == 1 && fake.recordedCode() == "auth-code-123"
	})

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.items) != 1 {
		t.Fatalf("expected 1 saved auth record, got %d", len(store.items))
	}
	var saved *coreauth.Auth
	for _, item := range store.items {
		saved = item
	}
	if saved == nil {
		t.Fatal("expected saved auth record")
	}
	if saved.Provider != "cline" {
		t.Fatalf("provider = %q, want cline", saved.Provider)
	}
	if saved.FileName != "cline-cline@example.com.json" {
		t.Fatalf("file name = %q, want cline-cline@example.com.json", saved.FileName)
	}
	if got := fake.recordedCode(); got != "auth-code-123" {
		t.Fatalf("exchange code = %q, want auth-code-123", got)
	}
	if got := fake.recordedRedirect(); got != "http://localhost:1455/callback" {
		t.Fatalf("redirect uri = %q, want http://localhost:1455/callback", got)
	}
}
