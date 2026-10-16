package logx

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestPrettyFormat(t *testing.T) {
	var buf bytes.Buffer
	off := false
	log := New(Options{Writer: &buf, Color: &off, Level: slog.LevelInfo}).With("job", "3d809ef3")
	log.Debug("hidden")
	log.Info("downloading", "downloaded", "12.4 MB", "files", 132, "took", 4200*time.Millisecond, "status", 200)
	log.Error("job failed", "err", errors.New("line one\nline two"))
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatal("debug line printed at info level")
	}
	for _, want := range []string{"INF downloading", "job=3d809ef3", "downloaded=12.4 MB", "files=132", "took=4.2s", "status=200", "ERR job failed", `err="line one\nline two"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("colour codes written with colour disabled")
	}
}

func TestJSONFormatAndLevels(t *testing.T) {
	var buf bytes.Buffer
	New(Options{Writer: &buf, Format: FormatJSON}).Info("hello", "n", 1)
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil || line["msg"] != "hello" {
		t.Fatalf("json output %q: %v", buf.String(), err)
	}
	for in, want := range map[string]slog.Level{"debug": slog.LevelDebug, "": slog.LevelInfo, "WARNING": slog.LevelWarn, "error": slog.LevelError} {
		if got, err := ParseLevel(in); err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("expected error for unknown level")
	}
}
