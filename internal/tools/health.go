package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type healthInput struct{}

func registerHealth(server *mcp.Server, deps *Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "tg_health",
		Description: "Check the MCP server's status: is the Telegram connection alive, " +
			"how long has the server been running, is auditing on. Use this if you " +
			"think a previous tool call failed because of a connection problem.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: ptrBool(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ healthInput) (*mcp.CallToolResult, any, error) {
		return handleHealth(ctx, deps), nil, nil
	})
}

func handleHealth(ctx context.Context, deps *Deps) *mcp.CallToolResult {
	var lines []string

	// Process-level connection state: tracked by the telegram client across
	// reconnects (vs. a fresh ping which only sees this moment). Reported
	// first so an operator can distinguish 'connection is up but slow' from
	// 'we are mid-reconnect'.
	transportConnected := true
	if deps.Health != nil {
		transportConnected = deps.Health.Connected()
		if transportConnected {
			lines = append(lines, "transport: connected")
		} else {
			lines = append(lines, "transport: disconnected (client is reconnecting)")
		}
	}

	// Connectivity probe: ping self via UsersGetUsers([InputUserSelf]). Goes
	// through the rate limiter middleware so a runaway health-checker can't
	// hammer Telegram. Time it so the response includes RPC latency.
	var account string
	connected := false
	pingStart := time.Now()
	users, err := deps.API.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
	latency := time.Since(pingStart)
	if err != nil {
		lines = append(lines, fmt.Sprintf("status: error (telegram ping failed: %v)", err))
	} else if len(users) > 0 {
		if u, ok := users[0].AsNotEmpty(); ok {
			connected = true
			if u.Username != "" {
				account = "@" + u.Username
			} else {
				account = fmt.Sprintf("user:%d", u.ID)
			}
		}
	}

	if connected {
		lines = append(lines, "status: ok")
		lines = append(lines, "account: "+account)
		lines = append(lines, fmt.Sprintf("ping_latency_ms: %d", latency.Milliseconds()))
	}

	// Uptime — set when the server starts. Zero StartTime means uptime
	// reporting is disabled, which keeps tests independent of main().
	if !deps.StartTime.IsZero() {
		uptime := time.Since(deps.StartTime).Round(time.Second)
		lines = append(lines, "uptime: "+uptime.String())
	}

	// Audit log status.
	if deps.Audit != nil {
		lines = append(lines, "audit: enabled")
	} else {
		lines = append(lines, "audit: disabled")
	}

	// Rate limit, so the LLM knows why some calls feel slow.
	if deps.Limits.Rate.RequestsPerSecond > 0 {
		lines = append(lines, fmt.Sprintf("rate_limit: %.1f rps (burst %d)",
			deps.Limits.Rate.RequestsPerSecond, deps.Limits.Rate.Burst))
	}

	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: strings.Join(lines, "\n")}},
	}
	// Surface as error if either signal indicates trouble. Connected means
	// the ping just succeeded; transportConnected means the reconnect loop
	// considers the connection alive across history. Both must hold.
	if !connected || !transportConnected {
		result.IsError = true
	}
	return result
}
