package auth

import "testing"

func TestClineAuthenticatorProvider(t *testing.T) {
	t.Parallel()

	authenticator := NewClineAuthenticator()
	if authenticator == nil {
		t.Fatal("NewClineAuthenticator() returned nil")
	}
	if got := authenticator.Provider(); got != "cline" {
		t.Fatalf("Provider() = %q, want %q", got, "cline")
	}
	if authenticator.RefreshLead() == nil {
		t.Fatal("RefreshLead() returned nil")
	}
}
