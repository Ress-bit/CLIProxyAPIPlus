package auth

import "testing"

func TestCodeBuddyIntlAuthenticatorProvider(t *testing.T) {
	t.Parallel()

	authenticator := NewCodeBuddyIntlAuthenticator()
	if authenticator == nil {
		t.Fatal("NewCodeBuddyIntlAuthenticator() returned nil")
	}
	if got := authenticator.Provider(); got != "codebuddy-intl" {
		t.Fatalf("Provider() = %q, want %q", got, "codebuddy-intl")
	}
	if authenticator.RefreshLead() == nil {
		t.Fatal("RefreshLead() returned nil")
	}
}
