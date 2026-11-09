package logging_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/logging"
)

func TestDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(logging.Options{Debug: true, Out: &buf})

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
	logger := logging.New(logging.Options{Out: &buf})

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
