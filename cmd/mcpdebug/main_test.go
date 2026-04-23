package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteResultFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_result.bin")
	want := []byte{0x01, 0x02, 0x03}

	if err := writeResultFile(path, want); err != nil {
		t.Fatalf("writeResultFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("written bytes mismatch: got %v want %v", got, want)
	}
}
