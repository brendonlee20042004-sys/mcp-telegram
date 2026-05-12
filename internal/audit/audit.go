// Package audit writes a tamper-evident JSONL trail of tool invocations.
//
// One line per tool call. Each line is a self-contained JSON object with
// timestamp, tool name, arguments (with long fields truncated), outcome,
// and call duration. The file is opened with O_APPEND and protected by a
// mutex so concurrent writes never interleave.
//
// The audit log is intended for security review and incident response:
// "what did the LLM do, and when". It is NOT a debug log — it deliberately
// records only invocation metadata, never secrets or internal state.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sync"
	"time"
)

// maxFieldLen caps the size of string fields in args after which they are
// truncated. Keeps a multi-megabyte message body from filling the audit log
// while still preserving "what kind of thing was sent".
const maxFieldLen = 200

// Logger is the audit sink. Nil-safe: a nil *Logger silently no-ops.
type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	closer io.Closer
}

// New opens path for append, sets 0600 permissions, and returns a Logger.
// If path is empty, returns nil — callers should treat that as "audit off".
func New(path string) (*Logger, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	// Re-enforce 0600 even if the file already existed with looser perms.
	_ = os.Chmod(path, 0600)
	return &Logger{w: f, closer: f}, nil
}

// NewWithWriter is for tests: any io.Writer becomes the audit sink.
func NewWithWriter(w io.Writer) *Logger {
	return &Logger{w: w}
}

// Close flushes and closes the underlying file. Safe to call on nil.
func (l *Logger) Close() error {
	if l == nil || l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// Record represents one entry written to the audit log.
type Record struct {
	Timestamp  string         `json:"ts"`
	Tool       string         `json:"tool"`
	Args       map[string]any `json:"args,omitempty"`
	Outcome    string         `json:"outcome"`
	Error      string         `json:"error,omitempty"`
	DurationMs int64          `json:"duration_ms"`
}

// Log writes one record. Args is any struct or map and is reflected into a
// flat map of strings with long fields truncated. ok indicates success;
// when false, errMsg is included.
//
// Returns the marshaling error if any but never panics. A nil *Logger
// returns nil immediately.
func (l *Logger) Log(toolName string, args any, ok bool, errMsg string, duration time.Duration) error {
	if l == nil {
		return nil
	}

	rec := Record{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Tool:       toolName,
		Args:       extractArgs(args),
		DurationMs: duration.Milliseconds(),
	}
	if ok {
		rec.Outcome = "ok"
	} else {
		rec.Outcome = "error"
		rec.Error = truncate(errMsg, maxFieldLen)
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal audit record: %w", err)
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(data)
	return err
}

// extractArgs reflects a struct or map into a flat map[string]any suitable
// for JSON. Long strings are truncated. Unsupported kinds become their Go
// type name as a string. Returns nil for nil input or empty results.
func extractArgs(args any) map[string]any {
	if args == nil {
		return nil
	}
	v := reflect.ValueOf(args)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	out := make(map[string]any)
	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			fld := t.Field(i)
			if !fld.IsExported() {
				continue
			}
			name := fieldName(fld)
			out[name] = sanitize(v.Field(i).Interface())
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			out[fmt.Sprint(k.Interface())] = sanitize(v.MapIndex(k).Interface())
		}
	default:
		// Single scalar — wrap it.
		out["value"] = sanitize(args)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// fieldName picks the JSON tag's name if present, else the Go field name.
func fieldName(f reflect.StructField) string {
	if tag, ok := f.Tag.Lookup("json"); ok && tag != "" && tag != "-" {
		// Strip ",omitempty" etc.
		for i, r := range tag {
			if r == ',' {
				return tag[:i]
			}
		}
		return tag
	}
	return f.Name
}

// sanitize truncates strings; everything else passes through unchanged so
// json.Marshal can serialize it naturally.
func sanitize(v any) any {
	if s, ok := v.(string); ok {
		return truncate(s, maxFieldLen)
	}
	return v
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}
