package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Prgebish/mcp-telegram/internal/audit"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolError(t *testing.T) {
	result := toolError("something went wrong")
	if !result.IsError {
		t.Error("expected IsError to be true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
}

func TestIsPathUnder(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0755)

	tests := []struct {
		name    string
		path    string
		allowed []string
		want    bool
	}{
		{"exact match", tmpDir, []string{tmpDir}, true},
		{"subdirectory", filepath.Join(tmpDir, "sub", "file.txt"), []string{tmpDir}, true},
		{"outside", "/etc/passwd", []string{tmpDir}, false},
		{"traversal attempt", filepath.Join(tmpDir, "..", "etc", "passwd"), []string{tmpDir}, false},
		{"empty allowed", tmpDir, nil, false},
		{"multiple allowed", filepath.Join(subDir, "file.txt"), []string{"/nonexistent", subDir}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPathUnder(tt.path, tt.allowed)
			if got != tt.want {
				t.Errorf("isPathUnder(%q, %v) = %v, want %v", tt.path, tt.allowed, got, tt.want)
			}
		})
	}
}

func TestIsPathUnder_SymlinkBypass(t *testing.T) {
	tmpDir := t.TempDir()
	allowed := filepath.Join(tmpDir, "allowed")
	os.MkdirAll(allowed, 0755)

	// Create a target outside the allowed directory.
	outside := filepath.Join(tmpDir, "outside")
	os.MkdirAll(outside, 0755)
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0600)

	// Create a symlink inside allowed that points outside.
	link := filepath.Join(allowed, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// /allowed/link/secret.txt should NOT be considered under /allowed
	// because the symlink resolves to /outside/secret.txt.
	if isPathUnder(filepath.Join(link, "secret.txt"), []string{allowed}) {
		t.Error("symlink bypass: path through symlink directory should not be considered under allowed dir")
	}

	// Leaf symlink: /allowed/secret_link -> /outside/secret.txt
	leafLink := filepath.Join(allowed, "secret_link")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), leafLink); err != nil {
		t.Skipf("cannot create leaf symlink: %v", err)
	}
	if isPathUnder(leafLink, []string{allowed}) {
		t.Error("symlink bypass: leaf symlink pointing outside should not be considered under allowed dir")
	}
}

func TestRecordAudit_NilLoggerNoOp(t *testing.T) {
	deps := &Deps{} // Audit is nil
	// Must not panic.
	recordAudit(deps, "tg_send", struct{ Chat string }{Chat: "@alice"}, time.Now(), nil)
}

func TestRecordAudit_NilDepsNoOp(t *testing.T) {
	recordAudit(nil, "tg_send", nil, time.Now(), nil)
}

func TestRecordAudit_SuccessResult(t *testing.T) {
	var buf bytes.Buffer
	deps := &Deps{Audit: audit.NewWithWriter(&buf)}
	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Message sent to @alice"}},
	}
	recordAudit(deps, "tg_send", struct{ Chat string }{Chat: "@alice"}, time.Now(), result)

	var rec audit.Record
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if rec.Tool != "tg_send" {
		t.Errorf("tool = %q", rec.Tool)
	}
	if rec.Outcome != "ok" {
		t.Errorf("outcome = %q, want ok", rec.Outcome)
	}
	if rec.Error != "" {
		t.Errorf("error should be empty on success, got %q", rec.Error)
	}
}

func TestRecordAudit_ErrorResult(t *testing.T) {
	var buf bytes.Buffer
	deps := &Deps{Audit: audit.NewWithWriter(&buf)}
	result := toolError("access denied: @alice does not have 'send' permission")
	recordAudit(deps, "tg_send", struct{ Chat string }{Chat: "@alice"}, time.Now(), result)

	var rec audit.Record
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec)
	if rec.Outcome != "error" {
		t.Errorf("outcome = %q, want error", rec.Outcome)
	}
	if !strings.Contains(rec.Error, "access denied") {
		t.Errorf("error = %q, want to contain 'access denied'", rec.Error)
	}
}

func TestRecordAudit_NilResultIsOk(t *testing.T) {
	// Some code paths may pass nil for "no result yet" — treat as success.
	var buf bytes.Buffer
	deps := &Deps{Audit: audit.NewWithWriter(&buf)}
	recordAudit(deps, "tg_me", nil, time.Now(), nil)
	if !strings.Contains(buf.String(), `"outcome":"ok"`) {
		t.Errorf("nil result should be ok, got: %s", buf.String())
	}
}

func TestPtrBool(t *testing.T) {
	v := ptrBool(true)
	if v == nil || *v != true {
		t.Error("ptrBool(true) should return pointer to true")
	}
	v = ptrBool(false)
	if v == nil || *v != false {
		t.Error("ptrBool(false) should return pointer to false")
	}
}
