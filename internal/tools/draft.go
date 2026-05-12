package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/Prgebish/mcp-telegram/internal/config"
	"github.com/gotd/td/tg"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type draftInput struct {
	Chat    string `json:"chat" jsonschema:"required,Chat reference: @username, user:ID, chat:ID, or channel:ID"`
	Text    string `json:"text" jsonschema:"required,Draft message text"`
	DryRun  bool   `json:"dry_run,omitempty" jsonschema:"If true, validate and ACL-check but skip actually saving the draft."`
	Confirm string `json:"confirm,omitempty" jsonschema:"Exact confirmation token required when the chat has require_confirm: true."`
}

func registerDraft(server *mcp.Server, deps *Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tg_draft",
		Description: "Save a draft message in a whitelisted chat. Does NOT send the message.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: ptrBool(false),
			IdempotentHint:  true,
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input draftInput) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		result := handleDraft(ctx, deps, input)
		recordAudit(deps, "tg_draft", input, start, result)
		return result, nil, nil
	})
}

func handleDraft(ctx context.Context, deps *Deps, input draftInput) *mcp.CallToolResult {
	peer, identity, err := deps.Resolver.ResolvePeerForTool(ctx, input.Chat)
	if err != nil {
		return toolError(fmt.Sprintf("cannot resolve chat: %v", err))
	}

	if !deps.ACL.Allowed(identity, config.PermDraft) {
		return toolError(fmt.Sprintf("access denied: %s does not have 'draft' permission", input.Chat))
	}

	if r := checkConfirm(deps, identity, config.PermDraft, input.Chat, "draft-in", input.Confirm); r != nil {
		return r
	}

	if input.DryRun {
		return dryRunResult(fmt.Sprintf("would save draft in %s", input.Chat))
	}

	_, err = deps.API.MessagesSaveDraft(ctx, &tg.MessagesSaveDraftRequest{
		Peer:    peer.InputPeer(),
		Message: input.Text,
	})
	if err != nil {
		return toolError(fmt.Sprintf("failed to save draft: %v", err))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Draft saved in %s", input.Chat)},
		},
	}
}
