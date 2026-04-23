package main

import (
	"testing"

	"google.golang.org/protobuf/types/dynamicpb"
)

func TestExecClientMessageDescriptorAvailable(t *testing.T) {
	msg := dynamicpb.NewMessage(execClientMessageDescriptor())
	if got := string(msg.Descriptor().Name()); got != "ExecClientMessage" {
		t.Fatalf("descriptor name mismatch: got %q", got)
	}
	if msg.Descriptor().Fields().Len() == 0 {
		t.Fatal("expected ExecClientMessage to have fields")
	}
}
