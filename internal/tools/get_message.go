package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/Prgebish/mcp-telegram/internal/config"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type getMessageInput struct {
	Chat      string `json:"chat" jsonschema:"required,Chat reference: @username, user:ID, chat:ID, or channel:ID"`
	MessageID int    `json:"message_id" jsonschema:"required,Numeric message ID to fetch"`
}

func registerGetMessage(server *mcp.Server, deps *Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tg_get_message",
		Description: "Fetch a single message by ID from a whitelisted chat. Useful when an LLM has a message ID (from tg_history, tg_search, or a deep link) and needs the message contents on its own.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: ptrBool(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input getMessageInput) (*mcp.CallToolResult, any, error) {
		return handleGetMessage(ctx, deps, input), nil, nil
	})
}

func handleGetMessage(ctx context.Context, deps *Deps, input getMessageInput) *mcp.CallToolResult {
	if input.MessageID <= 0 {
		return toolError("message_id must be a positive integer")
	}

	chatPeer, identity, err := deps.Resolver.ResolvePeerForTool(ctx, input.Chat)
	if err != nil {
		return toolError(fmt.Sprintf("cannot resolve chat: %v", err))
	}

	if !deps.ACL.Allowed(identity, config.PermRead) {
		return toolError(fmt.Sprintf("access denied: %s does not have 'read' permission", input.Chat))
	}

	// Channels and supergroups use channels.getMessages (needs an InputChannel),
	// everything else uses messages.getMessages.
	var result tg.MessagesMessagesClass
	if ch, ok := chatPeer.(ChannelPeer); ok {
		result, err = deps.API.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: ch.InputChannel(),
			ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: input.MessageID}},
		})
	} else {
		result, err = deps.API.MessagesGetMessages(ctx, []tg.InputMessageClass{
			&tg.InputMessageID{ID: input.MessageID},
		})
	}
	if err != nil {
		return toolError(fmt.Sprintf("failed to fetch message: %v", err))
	}

	modified, ok := result.AsModified()
	if !ok {
		return toolError("unexpected response from Telegram (messagesNotModified)")
	}

	messages := modified.GetMessages()
	if len(messages) == 0 {
		return toolError(fmt.Sprintf("message %d not found in %s", input.MessageID, input.Chat))
	}

	// MessagesGetMessages returns MessageEmpty for IDs that don't exist
	// or belong to a different chat — treat that the same as "not found".
	msg, ok := messages[0].(*tg.Message)
	if !ok {
		return toolError(fmt.Sprintf("message %d not found in %s", input.MessageID, input.Chat))
	}

	entities := buildEntities(modified.GetUsers(), modified.GetChats())
	from := resolveFromName(msg, chatPeer.InputPeer(), entities)
	ts := time.Unix(int64(msg.Date), 0).In(time.Local).Format("2006-01-02 15:04")

	text := msg.Message
	dl := downloader.NewDownloader()
	if text == "" && msg.Media != nil {
		text = formatMedia(ctx, deps, dl, msg, chatPeer.InputPeer())
	} else if msg.Media != nil {
		text += " " + formatMedia(ctx, deps, dl, msg, chatPeer.InputPeer())
	}
	if text == "" {
		text = "[empty]"
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("[%d] %s (%s): %s", msg.ID, from, ts, text)},
		},
	}
}

// buildEntities groups users and chats from a Messages response into the
// peer.Entities helper used by resolveFromName/formatMedia.
func buildEntities(users []tg.UserClass, chats []tg.ChatClass) peer.Entities {
	usersMap := make(map[int64]*tg.User, len(users))
	for _, u := range users {
		if user, ok := u.AsNotEmpty(); ok {
			usersMap[user.ID] = user
		}
	}
	chatsMap := make(map[int64]*tg.Chat)
	channelsMap := make(map[int64]*tg.Channel)
	for _, c := range chats {
		switch v := c.(type) {
		case *tg.Chat:
			chatsMap[v.ID] = v
		case *tg.Channel:
			channelsMap[v.ID] = v
		}
	}
	return peer.NewEntities(usersMap, chatsMap, channelsMap)
}
