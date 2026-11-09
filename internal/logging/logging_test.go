package logging_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/logging"
)

func TestDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(logging.Options{Debug: true, Format: "text", Out: &buf})

	logger.Debug("upload started", "versionCode", 42)

	out := buf.String()
	if !strings.Contains(out, "level=DEBUG") {
		t.Fatalf("debug line missing DEBUG level: %q", out)
	}
	if !strings.Contains(out, "versionCode=42") {
		t.Fatalf("debug line missing attribute: %q", out)
	}
}

func TestInfoLevelHidesDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(logging.Options{Format: "text", Out: &buf})

	logger.Debug("hidden")
	logger.Info("visible")

	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("debug line leaked at info level: %q", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("info line missing: %q", out)
	}
}

func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(logging.Options{Format: "json", Out: &buf})

	logger.Info("published", "track", "internal")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
	}
	if got["msg"] != "published" || got["track"] != "internal" {
		t.Fatalf("unexpected log record: %#v", got)
	}
}

func TestValidFormat(t *testing.T) {
	for _, f := range []string{"text", "json"} {
		if !logging.ValidFormat(f) {
			t.Errorf("ValidFormat(%q) = false, want true", f)
		}
	}
	for _, f := range []string{"", "yaml", "TEXT"} {
		if logging.ValidFormat(f) {
			t.Errorf("ValidFormat(%q) = true, want false", f)
		}
	}
}
