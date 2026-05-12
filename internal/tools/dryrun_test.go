package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/Prgebish/mcp-telegram/internal/config"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

// failOnRPCClient returns a tg.Client that calls t.Fatal on any RPC.
// Useful for dry-run tests that must not touch Telegram.
func failOnRPCClient(t *testing.T) *tg.Client {
	t.Helper()
	return mockTgClient(func(_ context.Context, input bin.Encoder, _ bin.Decoder) error {
		t.Errorf("dry-run must not invoke Telegram RPC, got %T", input)
		return nil
	})
}

// --- tg_send ---

func TestHandleSend_DryRun_Text(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), failOnRPCClient(t), config.PermSend)
	result := handleSend(context.Background(), deps, sendInput{
		Chat: "@testchat", Text: "hello world", DryRun: true,
	})
	if result.IsError {
		t.Fatalf("dry-run should succeed: %s", resultText(result))
	}
	text := resultText(result)
	if !strings.HasPrefix(text, "DRY-RUN:") {
		t.Errorf("expected DRY-RUN prefix, got: %s", text)
	}
	for _, want := range []string{"would send", "@testchat", "hello world"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

func TestHandleSend_DryRun_StillEnforcesACL(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), failOnRPCClient(t)) // no PermSend
	result := handleSend(context.Background(), deps, sendInput{
		Chat: "@testchat", Text: "hello", DryRun: true,
	})
	if !result.IsError {
		t.Fatal("dry-run must still enforce ACL")
	}
	if !strings.Contains(resultText(result), "'send' permission") {
		t.Errorf("expected ACL denial, got: %s", resultText(result))
	}
}

func TestHandleSend_DryRun_TruncatesLongText(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), failOnRPCClient(t), config.PermSend)
	long := strings.Repeat("x", 200)
	result := handleSend(context.Background(), deps, sendInput{
		Chat: "@testchat", Text: long, DryRun: true,
	})
	text := resultText(result)
	if !strings.Contains(text, "...") {
		t.Errorf("expected truncation marker, got: %s", text)
	}
}

func TestHandleSend_DryRun_WithReplyTo(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), failOnRPCClient(t), config.PermSend)
	result := handleSend(context.Background(), deps, sendInput{
		Chat: "@testchat", Text: "hi", ReplyTo: "42", DryRun: true,
	})
	if !strings.Contains(resultText(result), "reply to message 42") {
		t.Errorf("expected reply info, got: %s", resultText(result))
	}
}

// --- tg_forward ---

func TestHandleForward_DryRun(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), failOnRPCClient(t), config.PermRead, config.PermSend)
	result := handleForward(context.Background(), deps, forwardInput{
		FromChat: "@testchat", ToChat: "@testchat",
		MessageIDs: "1,2,3", DryRun: true,
	})
	if result.IsError {
		t.Fatalf("dry-run should succeed: %s", resultText(result))
	}
	text := resultText(result)
	for _, want := range []string{"DRY-RUN:", "would forward 3 message(s)", "@testchat"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

func TestHandleForward_DryRun_ACLStillEnforced(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), failOnRPCClient(t), config.PermSend) // missing read
	result := handleForward(context.Background(), deps, forwardInput{
		FromChat: "@testchat", ToChat: "@testchat",
		MessageIDs: "1", DryRun: true,
	})
	if !result.IsError {
		t.Fatal("dry-run must still enforce ACL")
	}
}

// --- tg_draft ---

func TestHandleDraft_DryRun(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), failOnRPCClient(t), config.PermDraft)
	result := handleDraft(context.Background(), deps, draftInput{
		Chat: "@testchat", Text: "draft text", DryRun: true,
	})
	if result.IsError {
		t.Fatalf("dry-run should succeed: %s", resultText(result))
	}
	text := resultText(result)
	if !strings.Contains(text, "DRY-RUN:") || !strings.Contains(text, "would save draft") {
		t.Errorf("expected dry-run description, got: %s", text)
	}
}

// --- tg_mark_read ---

func TestHandleMarkRead_DryRun(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), failOnRPCClient(t), config.PermMarkRead)
	result := handleMarkRead(context.Background(), deps, markReadInput{
		Chat: "@testchat", DryRun: true,
	})
	if result.IsError {
		t.Fatalf("dry-run should succeed: %s", resultText(result))
	}
	text := resultText(result)
	if !strings.Contains(text, "DRY-RUN:") || !strings.Contains(text, "would mark") {
		t.Errorf("expected dry-run description, got: %s", text)
	}
}

func TestHandleMarkRead_DryRun_OnChannel(t *testing.T) {
	deps := newTestDeps(newChannelResolver(), failOnRPCClient(t), config.PermMarkRead)
	result := handleMarkRead(context.Background(), deps, markReadInput{
		Chat: "@testchat", DryRun: true,
	})
	if result.IsError {
		t.Fatalf("dry-run on channel should succeed: %s", resultText(result))
	}
}
