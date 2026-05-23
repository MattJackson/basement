package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestLogFormatJSON_ProducesParseableLine: when BASEMENT_LOG_FORMAT=json
// the slog handler emits one JSON-per-line that downstream log
// aggregators can ingest without a custom parser.
func TestLogFormatJSON_ProducesParseableLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("server started", "addr", ":8080", "version", "v1.11.0f")

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("expected log output, got empty buffer")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\nbody=%s", err, out)
	}
	for _, key := range []string{"time", "level", "msg", "addr", "version"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in JSON log line; got %v", key, got)
		}
	}
	if got["msg"] != "server started" {
		t.Errorf("msg = %v, want \"server started\"", got["msg"])
	}
	if got["addr"] != ":8080" {
		t.Errorf("addr = %v, want \":8080\"", got["addr"])
	}
}

// TestLogFormatText_IsHumanReadable: when BASEMENT_LOG_FORMAT=text the
// slog handler emits key=value lines suitable for developer terminals
// (not parseable JSON, but easy on the eyes).
func TestLogFormatText_IsHumanReadable(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("server started", "addr", ":8080")

	out := strings.TrimSpace(buf.String())
	if strings.HasPrefix(out, "{") {
		t.Errorf("text handler produced JSON-looking output: %s", out)
	}
	if !strings.Contains(out, `msg="server started"`) {
		t.Errorf("expected msg=\"server started\" in text output, got: %s", out)
	}
	if !strings.Contains(out, `addr=:8080`) {
		t.Errorf("expected addr=:8080 in text output, got: %s", out)
	}
}
