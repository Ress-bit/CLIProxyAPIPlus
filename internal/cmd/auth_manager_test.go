package cmd

import (
	"context"
	"testing"
)

func TestNewAuthManager_RegistersCodeBuddyIntlAuthenticator(t *testing.T) {
	t.Parallel()

	manager := newAuthManager()
	if manager == nil {
		t.Fatal("newAuthManager() returned nil")
	}

	_, _, err := manager.Login(context.Background(), "codebuddy-intl", nil, nil)
	if err == nil {
		t.Fatal("expected login error from authenticator, got nil")
	}
	if err.Error() == "cliproxy auth: authenticator codebuddy-intl not registered" {
		t.Fatalf("codebuddy-intl authenticator was not registered: %v", err)
	}
	if err.Error() != "codebuddy-intl: configuration is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewAuthManager_RegistersClineAuthenticator(t *testing.T) {
	t.Parallel()

	manager := newAuthManager()
	if manager == nil {
		t.Fatal("newAuthManager() returned nil")
	}

	_, _, err := manager.Login(context.Background(), "cline", nil, nil)
	if err == nil {
		t.Fatal("expected login error from authenticator, got nil")
	}
	if err.Error() == "cliproxy auth: authenticator cline not registered" {
		t.Fatalf("cline authenticator was not registered: %v", err)
	}
	if err.Error() != "cliproxy auth: configuration is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}
