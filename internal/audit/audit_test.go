package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNew_EmptyPathReturnsNil(t *testing.T) {
	l, err := New("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l != nil {
		t.Error("expected nil logger for empty path")
	}
}

func TestNew_CreatesFileWith0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestNew_EnforcesModeOnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	// Create with loose perms first.
	if err := os.WriteFile(path, []byte("preexisting\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	l, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want 0600 (enforced)", info.Mode().Perm())
	}
}

func TestLog_NilLoggerIsNoOp(t *testing.T) {
	var l *Logger
	if err := l.Log("anything", nil, true, "", 0); err != nil {
		t.Errorf("nil logger should return nil, got %v", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("Close on nil: %v", err)
	}
}

func TestLog_BasicStructArgs(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)

	type input struct {
		Chat string `json:"chat"`
		Text string `json:"text,omitempty"`
	}
	if err := l.Log("tg_send", input{Chat: "@alice", Text: "hi"}, true, "", 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	var rec Record
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}
	if rec.Tool != "tg_send" {
		t.Errorf("tool = %q", rec.Tool)
	}
	if rec.Outcome != "ok" {
		t.Errorf("outcome = %q", rec.Outcome)
	}
	if rec.Args["chat"] != "@alice" {
		t.Errorf("args.chat = %v", rec.Args["chat"])
	}
	if rec.Args["text"] != "hi" {
		t.Errorf("args.text = %v", rec.Args["text"])
	}
	if rec.DurationMs != 250 {
		t.Errorf("duration_ms = %d, want 250", rec.DurationMs)
	}
	if rec.Error != "" {
		t.Errorf("error should be empty on ok, got %q", rec.Error)
	}
}

func TestLog_ErrorOutcome(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)

	type input struct{ Chat string }
	if err := l.Log("tg_send", input{Chat: "@bob"}, false, "access denied", time.Millisecond); err != nil {
		t.Fatal(err)
	}

	var rec Record
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec)
	if rec.Outcome != "error" {
		t.Errorf("outcome = %q, want error", rec.Outcome)
	}
	if rec.Error != "access denied" {
		t.Errorf("error = %q", rec.Error)
	}
}

func TestLog_TruncatesLongStrings(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)

	long := strings.Repeat("x", 1000)
	type input struct{ Text string }
	if err := l.Log("tg_send", input{Text: long}, true, "", 0); err != nil {
		t.Fatal(err)
	}

	var rec Record
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec)
	got, _ := rec.Args["Text"].(string)
	if !strings.HasSuffix(got, "...[truncated]") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	if len(got) > maxFieldLen+len("...[truncated]") {
		t.Errorf("got len=%d, exceeds bound", len(got))
	}
}

func TestLog_TruncatesLongError(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	long := strings.Repeat("e", 500)
	_ = l.Log("tg_send", nil, false, long, 0)

	var rec Record
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec)
	if !strings.HasSuffix(rec.Error, "...[truncated]") {
		t.Errorf("error not truncated: %q", rec.Error)
	}
}

func TestLog_NilArgsOmitted(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	if err := l.Log("tg_me", nil, true, "", 0); err != nil {
		t.Fatal(err)
	}
	// "args" with omitempty should not appear when nil
	if strings.Contains(buf.String(), `"args"`) {
		t.Errorf("args should be omitted when nil; got: %s", buf.String())
	}
}

func TestLog_MapArgs(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	args := map[string]any{"chat": "@alice", "count": 42}
	if err := l.Log("tg_history", args, true, "", 0); err != nil {
		t.Fatal(err)
	}
	var rec Record
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec)
	if rec.Args["chat"] != "@alice" {
		t.Errorf("chat = %v", rec.Args["chat"])
	}
	// JSON unmarshals numbers as float64
	if v, _ := rec.Args["count"].(float64); v != 42 {
		t.Errorf("count = %v", rec.Args["count"])
	}
}

func TestLog_PointerToStruct(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	type input struct{ Chat string }
	if err := l.Log("tg_send", &input{Chat: "@alice"}, true, "", 0); err != nil {
		t.Fatal(err)
	}
	var rec Record
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec)
	if rec.Args["Chat"] != "@alice" {
		t.Errorf("chat = %v", rec.Args["Chat"])
	}
}

func TestLog_NilPointerIsSafe(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	type input struct{ Chat string }
	var p *input
	if err := l.Log("tg_send", p, true, "", 0); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), `"args"`) {
		t.Errorf("nil pointer should omit args, got: %s", buf.String())
	}
}

func TestLog_JSONLOneLinePerRecord(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	for i := 0; i < 3; i++ {
		_ = l.Log("tg_me", nil, true, "", 0)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3", len(lines))
	}
	for i, line := range lines {
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d not valid JSON: %v", i, err)
		}
	}
}

func TestLog_ConcurrentWritesAreAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	const goroutines = 50
	const perGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = l.Log("tg_send",
					struct{ Chat string }{Chat: "@chat"},
					true, "", time.Microsecond)
			}
		}(g)
	}
	wg.Wait()

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	want := goroutines * perGoroutine
	if len(lines) != want {
		t.Errorf("got %d lines, want %d", len(lines), want)
	}
	for i, line := range lines {
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d corrupted: %v\n%q", i, err, line)
		}
	}
}

func TestLog_IgnoresUnexportedFields(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	type input struct {
		Chat   string
		secret string
	}
	_ = l.Log("tg_send", input{Chat: "@alice", secret: "TOPSECRET"}, true, "", 0)
	if strings.Contains(buf.String(), "TOPSECRET") {
		t.Error("unexported field leaked into audit log")
	}
}

func TestLog_RespectsJSONDashTag(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf)
	type input struct {
		Chat string `json:"chat"`
		// json:"-" should hide field; sanitize by tag name "-"
		APIHash string `json:"api_hash"`
	}
	_ = l.Log("internal", input{Chat: "@alice", APIHash: "ABCDEF"}, true, "", 0)
	// We don't actively filter "-" tags yet — this test documents current behavior.
	// (Plain field name "api_hash" is exposed; callers must not pass secrets in args.)
	if !strings.Contains(buf.String(), `"chat":"@alice"`) {
		t.Errorf("chat missing: %s", buf.String())
	}
}
