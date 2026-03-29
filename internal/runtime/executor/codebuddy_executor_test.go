package executor

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codebuddy"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyCredentials_DefaultsToExternalDomain(t *testing.T) {
	t.Parallel()

	auth := &cliproxyauth.Auth{Metadata: map[string]any{}}
	_, _, domain := codeBuddyCredentials(auth)

	if domain != codebuddy.ExternalDomain {
		t.Fatalf("expected %q, got %q", codebuddy.ExternalDomain, domain)
	}
}

func TestCodeBuddyCredentials_UsesMetadataDomainWhenPresent(t *testing.T) {
	t.Parallel()

	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		"domain": "tenant.codebuddy.example",
	}}

	_, _, domain := codeBuddyCredentials(auth)

	if domain != "tenant.codebuddy.example" {
		t.Fatalf("expected metadata domain to win, got %q", domain)
	}
}

func TestCodeBuddyPrepareRequest_UsesExternalHeaders(t *testing.T) {
	t.Parallel()

	e := &CodeBuddyExecutor{}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		"access_token": "test-access-token",
		"user_id":      "user-123",
	}}
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v2/chat/completions", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	if err := e.PrepareRequest(req, auth); err != nil {
		t.Fatalf("PrepareRequest() error = %v", err)
	}

	if got := req.Header.Get("Authorization"); got != "Bearer test-access-token" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer test-access-token")
	}
	if got := req.Header.Get("X-Domain"); got != codebuddy.ExternalDomain {
		t.Fatalf("X-Domain = %q, want %q", got, codebuddy.ExternalDomain)
	}
	if got := req.Header.Get("X-User-Id"); got != "user-123" {
		t.Fatalf("X-User-Id = %q, want %q", got, "user-123")
	}
}

func TestNormalizeCodeBuddyModelStripsPrefix(t *testing.T) {
	t.Parallel()

	if got := normalizeCodeBuddyModel("codebuddy/gpt-5.4"); got != "gpt-5.4" {
		t.Fatalf("expected prefixed CodeBuddy model to normalize, got %q", got)
	}
	if got := normalizeCodeBuddyModel("gpt-5.4"); got != "gpt-5.4" {
		t.Fatalf("expected raw CodeBuddy model to stay unchanged, got %q", got)
	}
}

func TestTranslateCodeBuddyPayload_UsesUnprefixedUpstreamModel(t *testing.T) {
	t.Parallel()

	translated := translateCodeBuddyPayload(
		sdktranslator.FromString("openai"),
		sdktranslator.FromString("openai"),
		normalizeCodeBuddyModel("codebuddy/gpt-5.4"),
		[]byte(`{"model":"codebuddy/gpt-5.4","messages":[{"role":"user","content":"hi"}]}`),
		false,
	)
	if got := gjson.GetBytes(translated, "model").String(); got != "gpt-5.4" {
		t.Fatalf("expected translated upstream model gpt-5.4, got %q", got)
	}
}
