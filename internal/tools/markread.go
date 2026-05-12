package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/Prgebish/mcp-telegram/internal/config"
	"github.com/gotd/td/tg"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type markReadInput struct {
	Chat   string `json:"chat" jsonschema:"required,Chat reference: @username, user:ID, chat:ID, or channel:ID"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"If true, validate and ACL-check but skip the actual mark-read RPC."`
}

func registerMarkRead(server *mcp.Server, deps *Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tg_mark_read",
		Description: "Mark all messages as read in a whitelisted chat.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: ptrBool(false),
			IdempotentHint:  true,
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input markReadInput) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		result := handleMarkRead(ctx, deps, input)
		recordAudit(deps, "tg_mark_read", input, start, result)
		return result, nil, nil
	})
}

func handleMarkRead(ctx context.Context, deps *Deps, input markReadInput) *mcp.CallToolResult {
	peer, identity, err := deps.Resolver.ResolvePeerForTool(ctx, input.Chat)
	if err != nil {
		return toolError(fmt.Sprintf("cannot resolve chat: %v", err))
	}

	if !deps.ACL.Allowed(identity, config.PermMarkRead) {
		return toolError(fmt.Sprintf("access denied: %s does not have 'mark_read' permission", input.Chat))
	}

	if input.DryRun {
		return dryRunResult(fmt.Sprintf("would mark %s as read", input.Chat))
	}

	switch p := peer.(type) {
	case ChannelPeer:
		_, err = deps.API.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{
			Channel: p.InputChannel(),
		})
	default:
		_, err = deps.API.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{
			Peer: peer.InputPeer(),
		})
	}
	if err != nil {
		return toolError(fmt.Sprintf("failed to mark as read: %v", err))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Marked as read: %s", input.Chat)},
		},
	}
}
