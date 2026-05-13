package executor

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/codebuddy"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
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

func TestCodeBuddyCredentials_UsesIntlDomainWhenBaseURLIsIntl(t *testing.T) {
	t.Parallel()

	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		"base_url": codebuddy.IntlBaseURL,
	}}

	_, _, domain := codeBuddyCredentials(auth)

	if domain != codebuddy.IntlDefaultDomain {
		t.Fatalf("expected %q, got %q", codebuddy.IntlDefaultDomain, domain)
	}
}

func TestNewCodeBuddyIntlExecutor_UsesIntlIdentifierAndBaseURL(t *testing.T) {
	t.Parallel()

	e := NewCodeBuddyIntlExecutor(nil)
	if got := e.Identifier(); got != "codebuddy-intl" {
		t.Fatalf("Identifier() = %q, want %q", got, "codebuddy-intl")
	}

	auth := &cliproxyauth.Auth{Metadata: map[string]any{}}
	if got := codeBuddyBaseURL(e, auth); got != codebuddy.IntlBaseURL {
		t.Fatalf("codeBuddyBaseURL() = %q, want %q", got, codebuddy.IntlBaseURL)
	}

	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v2/chat/completions", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	auth.Metadata["access_token"] = "test-access-token"
	auth.Metadata["user_id"] = "user-123"
	if err := e.PrepareRequest(req, auth); err != nil {
		t.Fatalf("PrepareRequest() error = %v", err)
	}
	if got := req.Header.Get("X-Domain"); got != codebuddy.IntlDefaultDomain {
		t.Fatalf("X-Domain = %q, want %q", got, codebuddy.IntlDefaultDomain)
	}
}
