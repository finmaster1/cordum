package audit

import (
	"strings"
	"testing"
)

func TestSIEMActionConstants_Values(t *testing.T) {
	t.Parallel()

	if SIEMActionChatBootstrapRegistered != "chat.bootstrap_registered" {
		t.Errorf("SIEMActionChatBootstrapRegistered = %q, want %q", SIEMActionChatBootstrapRegistered, "chat.bootstrap_registered")
	}
	if SIEMActionChatSessionStarted != "chat.session_started" {
		t.Errorf("SIEMActionChatSessionStarted = %q, want %q", SIEMActionChatSessionStarted, "chat.session_started")
	}
	if SIEMActionChatSessionClosed != "chat.session_closed" {
		t.Errorf("SIEMActionChatSessionClosed = %q, want %q", SIEMActionChatSessionClosed, "chat.session_closed")
	}
}

func TestSIEMActionConstants_Family(t *testing.T) {
	t.Parallel()

	for _, action := range []string{
		SIEMActionChatBootstrapRegistered,
		SIEMActionChatSessionStarted,
		SIEMActionChatSessionClosed,
	} {
		if !strings.HasPrefix(action, "chat.") {
			t.Errorf("action %q should use chat. prefix", action)
		}
	}
}
