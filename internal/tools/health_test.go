package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Prgebish/mcp-telegram/internal/audit"
	"github.com/Prgebish/mcp-telegram/internal/config"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

func TestHandleHealth_ConnectedWithUsername(t *testing.T) {
	api := mockTgClient(func(_ context.Context, _ bin.Encoder, output bin.Decoder) error {
		v := output.(*tg.UserClassVector)
		v.Elems = []tg.UserClass{&tg.User{ID: 12345, FirstName: "Alice", Username: "alice"}}
		return nil
	})
	deps := &Deps{
		API:       api,
		StartTime: time.Now().Add(-2 * time.Minute),
		Limits:    config.LimitsConfig{Rate: config.RateConfig{RequestsPerSecond: 2.0, Burst: 3}},
	}
	result := handleHealth(context.Background(), deps)
	if result.IsError {
		t.Fatalf("connected health should not be IsError: %s", resultText(result))
	}
	text := resultText(result)
	for _, want := range []string{"status: ok", "account: @alice", "uptime:", "rate_limit: 2.0 rps", "audit: disabled"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in output: %s", want, text)
		}
	}
}

func TestHandleHealth_ConnectedNoUsername(t *testing.T) {
	api := mockTgClient(func(_ context.Context, _ bin.Encoder, output bin.Decoder) error {
		v := output.(*tg.UserClassVector)
		v.Elems = []tg.UserClass{&tg.User{ID: 99, FirstName: "Bob"}}
		return nil
	})
	deps := &Deps{API: api}
	result := handleHealth(context.Background(), deps)
	if result.IsError {
		t.Fatalf("connected health should not be IsError: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "account: user:99") {
		t.Errorf("expected fallback to user:ID, got: %s", resultText(result))
	}
}

func TestHandleHealth_PingFails(t *testing.T) {
	deps := &Deps{API: errorTgClient("connection refused")}
	result := handleHealth(context.Background(), deps)
	if !result.IsError {
		t.Fatal("expected IsError when ping fails")
	}
	text := resultText(result)
	if !strings.Contains(text, "status: error") {
		t.Errorf("missing 'status: error': %s", text)
	}
	if !strings.Contains(text, "connection refused") {
		t.Errorf("missing original error: %s", text)
	}
}

func TestHandleHealth_AuditEnabled(t *testing.T) {
	api := mockTgClient(func(_ context.Context, _ bin.Encoder, output bin.Decoder) error {
		v := output.(*tg.UserClassVector)
		v.Elems = []tg.UserClass{&tg.User{ID: 1, FirstName: "X", Username: "x"}}
		return nil
	})
	var buf bytes.Buffer
	deps := &Deps{API: api, Audit: audit.NewWithWriter(&buf)}
	result := handleHealth(context.Background(), deps)
	if !strings.Contains(resultText(result), "audit: enabled") {
		t.Errorf("expected 'audit: enabled': %s", resultText(result))
	}
}

func TestHandleHealth_ZeroStartTimeHidesUptime(t *testing.T) {
	api := mockTgClient(func(_ context.Context, _ bin.Encoder, output bin.Decoder) error {
		v := output.(*tg.UserClassVector)
		v.Elems = []tg.UserClass{&tg.User{ID: 1, FirstName: "X", Username: "x"}}
		return nil
	})
	deps := &Deps{API: api} // StartTime is zero
	if strings.Contains(resultText(handleHealth(context.Background(), deps)), "uptime") {
		t.Error("zero StartTime should hide uptime line")
	}
}

func TestHandleHealth_EmptyUserFallsBackToError(t *testing.T) {
	// UsersGetUsers returns empty UserEmpty — connection works but self is malformed.
	// We treat this as "not connected" since we can't identify ourselves.
	api := mockTgClient(func(_ context.Context, _ bin.Encoder, output bin.Decoder) error {
		v := output.(*tg.UserClassVector)
		v.Elems = []tg.UserClass{&tg.UserEmpty{ID: 1}}
		return nil
	})
	deps := &Deps{API: api}
	result := handleHealth(context.Background(), deps)
	if !result.IsError {
		t.Error("empty user response should be treated as error")
	}
}
