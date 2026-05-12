package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/Prgebish/mcp-telegram/internal/acl"
	"github.com/Prgebish/mcp-telegram/internal/config"
)

// confirmResolver returns a peer whose identity matches the chat ref used by
// the acl rule (@boss). Tests use it together with a checker that has
// require_confirm:true on @boss.
func confirmResolver(username string) *mockResolver {
	return &mockResolver{
		peer:     &mockPeer{inputPeer: nil},
		identity: acl.PeerIdentity{Kind: acl.KindUser, ID: 1, Username: username},
	}
}

func confirmRequiredDeps(t *testing.T, perms ...config.Permission) *Deps {
	t.Helper()
	checker, err := acl.NewChecker(config.ACLConfig{
		Chats: []config.ChatRule{
			{Match: "@boss", Permissions: perms, RequireConfirm: true},
		},
	})
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return &Deps{
		Resolver: confirmResolver("boss"),
		ACL:      checker,
		Limits:   config.LimitsConfig{MaxMessagesPerRequest: 50},
	}
}

func TestExpectedConfirmToken_Format(t *testing.T) {
	got := expectedConfirmToken("send-to", "@alice")
	want := "yes-send-to-@alice"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestACL_RequiresConfirm(t *testing.T) {
	checker, _ := acl.NewChecker(config.ACLConfig{
		Chats: []config.ChatRule{
			{Match: "@boss", Permissions: []config.Permission{config.PermSend}, RequireConfirm: true},
			{Match: "@friend", Permissions: []config.Permission{config.PermSend}},
		},
	})
	boss := acl.PeerIdentity{Kind: acl.KindUser, ID: 1, Username: "boss"}
	friend := acl.PeerIdentity{Kind: acl.KindUser, ID: 2, Username: "friend"}

	if !checker.RequiresConfirm(boss, config.PermSend) {
		t.Error("@boss should require confirm for send")
	}
	if checker.RequiresConfirm(friend, config.PermSend) {
		t.Error("@friend should NOT require confirm")
	}
	// Permission not granted -> no confirm needed
	if checker.RequiresConfirm(boss, config.PermDraft) {
		t.Error("perm not granted: confirm should not be required")
	}
}

func TestACL_RequiresConfirm_DenyRulesIgnored(t *testing.T) {
	// A deny rule with require_confirm should NOT activate the confirm path —
	// deny already blocks the action; confirm is meaningless.
	checker, _ := acl.NewChecker(config.ACLConfig{
		Chats: []config.ChatRule{
			{Match: "@boss", Permissions: []config.Permission{config.PermSend}, RequireConfirm: true, Deny: true},
		},
	})
	boss := acl.PeerIdentity{Kind: acl.KindUser, ID: 1, Username: "boss"}
	if checker.RequiresConfirm(boss, config.PermSend) {
		t.Error("require_confirm on a deny rule should be ignored")
	}
}

// --- tg_send ---

func TestHandleSend_Confirm_MissingToken(t *testing.T) {
	deps := confirmRequiredDeps(t, config.PermSend)
	result := handleSend(context.Background(), deps, sendInput{
		Chat: "@boss", Text: "hi",
	})
	if !result.IsError {
		t.Fatal("expected error when confirm missing")
	}
	text := resultText(result)
	if !strings.Contains(text, "confirmation required") {
		t.Errorf("error should mention confirmation: %s", text)
	}
	if !strings.Contains(text, "yes-send-to-@boss") {
		t.Errorf("error should print the exact expected token: %s", text)
	}
}

func TestHandleSend_Confirm_WrongToken(t *testing.T) {
	deps := confirmRequiredDeps(t, config.PermSend)
	result := handleSend(context.Background(), deps, sendInput{
		Chat: "@boss", Text: "hi", Confirm: "yes",
	})
	if !result.IsError {
		t.Fatal("expected error for wrong token")
	}
}

func TestHandleSend_Confirm_CorrectToken_DryRunOK(t *testing.T) {
	deps := confirmRequiredDeps(t, config.PermSend)
	// Use dry-run so we don't need to mock the Telegram RPC for success.
	result := handleSend(context.Background(), deps, sendInput{
		Chat: "@boss", Text: "hi", DryRun: true,
		Confirm: "yes-send-to-@boss",
	})
	if result.IsError {
		t.Fatalf("correct token should pass: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "DRY-RUN:") {
		t.Errorf("expected dry-run output: %s", resultText(result))
	}
}

func TestHandleSend_NoConfirmRequired_PassesWithoutToken(t *testing.T) {
	// @testchat is the default mock; not RequireConfirm.
	deps := newTestDeps(newAllowedResolver(), nil, config.PermSend)
	result := handleSend(context.Background(), deps, sendInput{
		Chat: "@testchat", Text: "hi", DryRun: true,
	})
	if result.IsError {
		t.Fatalf("non-confirm chat should not require token: %s", resultText(result))
	}
}

// --- tg_forward ---

func TestHandleForward_Confirm_MissingToken(t *testing.T) {
	deps := confirmRequiredDeps(t, config.PermRead, config.PermSend)
	result := handleForward(context.Background(), deps, forwardInput{
		FromChat: "@boss", ToChat: "@boss", MessageIDs: "1",
	})
	if !result.IsError {
		t.Fatal("expected error when confirm missing")
	}
	if !strings.Contains(resultText(result), "yes-forward-to-@boss") {
		t.Errorf("expected forward-to token in error: %s", resultText(result))
	}
}

func TestHandleForward_Confirm_CorrectToken_DryRun(t *testing.T) {
	deps := confirmRequiredDeps(t, config.PermRead, config.PermSend)
	result := handleForward(context.Background(), deps, forwardInput{
		FromChat: "@boss", ToChat: "@boss", MessageIDs: "1",
		DryRun: true, Confirm: "yes-forward-to-@boss",
	})
	if result.IsError {
		t.Fatalf("correct token should pass: %s", resultText(result))
	}
}

// --- tg_draft ---

func TestHandleDraft_Confirm_MissingToken(t *testing.T) {
	deps := confirmRequiredDeps(t, config.PermDraft)
	result := handleDraft(context.Background(), deps, draftInput{
		Chat: "@boss", Text: "x",
	})
	if !result.IsError {
		t.Fatal("expected error when confirm missing")
	}
	if !strings.Contains(resultText(result), "yes-draft-in-@boss") {
		t.Errorf("expected draft-in token in error: %s", resultText(result))
	}
}

// --- tg_mark_read ---

func TestHandleMarkRead_Confirm_MissingToken(t *testing.T) {
	deps := confirmRequiredDeps(t, config.PermMarkRead)
	result := handleMarkRead(context.Background(), deps, markReadInput{
		Chat: "@boss",
	})
	if !result.IsError {
		t.Fatal("expected error when confirm missing")
	}
	if !strings.Contains(resultText(result), "yes-mark-read-@boss") {
		t.Errorf("expected mark-read token in error: %s", resultText(result))
	}
}
