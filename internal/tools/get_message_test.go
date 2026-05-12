package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/Prgebish/mcp-telegram/internal/config"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

func TestHandleGetMessage_InvalidID(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), nil, config.PermRead)
	for _, id := range []int{0, -1, -100} {
		result := handleGetMessage(context.Background(), deps, getMessageInput{
			Chat: "@testchat", MessageID: id,
		})
		if !result.IsError {
			t.Fatalf("id=%d: expected error", id)
		}
		if text := resultText(result); !strings.Contains(text, "message_id must be a positive integer") {
			t.Errorf("id=%d: error text = %q", id, text)
		}
	}
}

func TestHandleGetMessage_ResolveError(t *testing.T) {
	deps := newTestDeps(newErrorResolver("peer not found"), nil, config.PermRead)
	result := handleGetMessage(context.Background(), deps, getMessageInput{
		Chat: "@unknown", MessageID: 42,
	})
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := resultText(result); !strings.Contains(text, "cannot resolve chat") {
		t.Errorf("error = %q, want 'cannot resolve chat'", text)
	}
}

func TestHandleGetMessage_ACLDenied(t *testing.T) {
	// Resolver returns testchat, but ACL has no read permission.
	deps := newTestDeps(newAllowedResolver(), nil) // no perms
	result := handleGetMessage(context.Background(), deps, getMessageInput{
		Chat: "@testchat", MessageID: 42,
	})
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := resultText(result); !strings.Contains(text, "'read' permission") {
		t.Errorf("error = %q, want 'read permission' denied", text)
	}
}

func TestHandleGetMessage_APIError(t *testing.T) {
	deps := newTestDeps(newAllowedResolver(), errorTgClient("flood wait"), config.PermRead)
	result := handleGetMessage(context.Background(), deps, getMessageInput{
		Chat: "@testchat", MessageID: 42,
	})
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := resultText(result); !strings.Contains(text, "flood wait") {
		t.Errorf("error = %q, want 'flood wait'", text)
	}
}

func TestHandleGetMessage_NotFound(t *testing.T) {
	api := mockTgClient(func(_ context.Context, _ bin.Encoder, output bin.Decoder) error {
		box := output.(*tg.MessagesMessagesBox)
		box.Messages = &tg.MessagesMessages{
			Messages: []tg.MessageClass{&tg.MessageEmpty{ID: 42}},
			Users:    []tg.UserClass{},
			Chats:    []tg.ChatClass{},
		}
		return nil
	})
	deps := newTestDeps(newAllowedResolver(), api, config.PermRead)
	result := handleGetMessage(context.Background(), deps, getMessageInput{
		Chat: "@testchat", MessageID: 42,
	})
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := resultText(result); !strings.Contains(text, "not found") {
		t.Errorf("error = %q, want 'not found'", text)
	}
}

func TestHandleGetMessage_EmptyResponse(t *testing.T) {
	api := mockTgClient(func(_ context.Context, _ bin.Encoder, output bin.Decoder) error {
		box := output.(*tg.MessagesMessagesBox)
		box.Messages = &tg.MessagesMessages{
			Messages: []tg.MessageClass{},
		}
		return nil
	})
	deps := newTestDeps(newAllowedResolver(), api, config.PermRead)
	result := handleGetMessage(context.Background(), deps, getMessageInput{
		Chat: "@testchat", MessageID: 42,
	})
	if !result.IsError {
		t.Fatal("expected error")
	}
	if text := resultText(result); !strings.Contains(text, "not found") {
		t.Errorf("error = %q, want 'not found'", text)
	}
}

func TestHandleGetMessage_SuccessUser(t *testing.T) {
	api := mockTgClient(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		// Verify the user/group branch is hit: should not be a channels request.
		if _, ok := input.(*tg.ChannelsGetMessagesRequest); ok {
			t.Error("user peer should not trigger ChannelsGetMessages")
		}
		box := output.(*tg.MessagesMessagesBox)
		box.Messages = &tg.MessagesMessages{
			Messages: []tg.MessageClass{
				&tg.Message{
					ID:      555,
					Date:    1700000000,
					Message: "hello world",
					FromID:  &tg.PeerUser{UserID: 100},
				},
			},
			Users: []tg.UserClass{&tg.User{ID: 100, FirstName: "Alice"}},
		}
		return nil
	})
	deps := newTestDeps(newAllowedResolver(), api, config.PermRead)
	result := handleGetMessage(context.Background(), deps, getMessageInput{
		Chat: "@testchat", MessageID: 555,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}
	text := resultText(result)
	for _, want := range []string{"[555]", "Alice", "hello world"} {
		if !strings.Contains(text, want) {
			t.Errorf("result = %q, want to contain %q", text, want)
		}
	}
}

func TestHandleGetMessage_SuccessChannel(t *testing.T) {
	var sawChannelsRequest bool
	api := mockTgClient(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		if _, ok := input.(*tg.ChannelsGetMessagesRequest); ok {
			sawChannelsRequest = true
		}
		box := output.(*tg.MessagesMessagesBox)
		box.Messages = &tg.MessagesChannelMessages{
			Messages: []tg.MessageClass{
				&tg.Message{
					ID:      999,
					Date:    1700000000,
					Message: "channel post",
				},
			},
			Chats: []tg.ChatClass{&tg.Channel{ID: 200, Title: "Test Channel", Broadcast: true}},
		}
		return nil
	})
	deps := newTestDeps(newChannelResolver(), api, config.PermRead)
	result := handleGetMessage(context.Background(), deps, getMessageInput{
		Chat: "@testchat", MessageID: 999,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}
	if !sawChannelsRequest {
		t.Error("channel peer should trigger ChannelsGetMessages, not MessagesGetMessages")
	}
	if text := resultText(result); !strings.Contains(text, "[999]") || !strings.Contains(text, "channel post") {
		t.Errorf("result = %q, missing fields", text)
	}
}

func TestHandleGetMessage_EmptyText(t *testing.T) {
	api := mockTgClient(func(_ context.Context, _ bin.Encoder, output bin.Decoder) error {
		box := output.(*tg.MessagesMessagesBox)
		box.Messages = &tg.MessagesMessages{
			Messages: []tg.MessageClass{
				&tg.Message{ID: 1, Date: 1700000000, FromID: &tg.PeerUser{UserID: 100}},
			},
			Users: []tg.UserClass{&tg.User{ID: 100, FirstName: "Bob"}},
		}
		return nil
	})
	deps := newTestDeps(newAllowedResolver(), api, config.PermRead)
	result := handleGetMessage(context.Background(), deps, getMessageInput{
		Chat: "@testchat", MessageID: 1,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}
	if text := resultText(result); !strings.Contains(text, "[empty]") {
		t.Errorf("result = %q, want '[empty]' for message with no text and no media", text)
	}
}
