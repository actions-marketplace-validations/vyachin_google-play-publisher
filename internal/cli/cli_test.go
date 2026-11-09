package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vyachin/google-play-publisher/internal/cli"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/version"
)

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errOut bytes.Buffer
	app := &cli.App{
		Stdout: &out,
		Stderr: &errOut,
		Now:    time.Now,
		Version: version.Info{
			Version: "test",
			Commit:  "abc123",
			Date:    "2026-11-09",
			Go:      "go1.27",
		},
	}

	code = cli.Execute(context.Background(), app, args)
	return out.String(), errOut.String(), code
}

func TestVersion(t *testing.T) {
	out, _, code := run(t, "version")

	if code != int(exitcode.OK) {
		t.Fatalf("exit code = %d, want %d", code, exitcode.OK)
	}
	if !strings.Contains(out, "google-play-publisher version test") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestVersionJSON(t *testing.T) {
	out, _, code := run(t, "--json", "version")

	if code != int(exitcode.OK) {
		t.Fatalf("exit code = %d, want %d", code, exitcode.OK)
	}

	var got version.Info
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, out)
	}
	if got.Version != "test" || got.Commit != "abc123" {
		t.Fatalf("unexpected JSON: %#v", got)
	}
}

func TestDebugFlagAccepted(t *testing.T) {
	_, _, code := run(t, "--debug", "version")
	if code != int(exitcode.OK) {
		t.Fatalf("exit code = %d, want %d", code, exitcode.OK)
	}
}

func TestUnknownCommand(t *testing.T) {
	_, errOut, code := run(t, "nope")

	if code != int(exitcode.Usage) {
		t.Fatalf("exit code = %d, want %d (stderr: %q)", code, exitcode.Usage, errOut)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func TestUnknownFlag(t *testing.T) {
	_, errOut, code := run(t, "--nope")

	if code != int(exitcode.Usage) {
		t.Fatalf("exit code = %d, want %d (stderr: %q)", code, exitcode.Usage, errOut)
	}
	if !strings.Contains(errOut, "unknown flag") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func TestUnexpectedArgument(t *testing.T) {
	_, _, code := run(t, "version", "extra")
	if code != int(exitcode.Usage) {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Usage)
	}
}

func TestInvalidLogFormat(t *testing.T) {
	_, errOut, code := run(t, "--log-format", "yaml", "version")

	if code != int(exitcode.Usage) {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(errOut, "invalid --log-format") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func TestNonPositiveTimeout(t *testing.T) {
	_, errOut, code := run(t, "--timeout", "0s", "version")

	if code != int(exitcode.Usage) {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(errOut, "--timeout must be positive") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func TestHelp(t *testing.T) {
	out, _, code := run(t, "--help")

	if code != int(exitcode.OK) {
		t.Fatalf("exit code = %d, want %d", code, exitcode.OK)
	}
	for _, want := range []string{"version", "completion"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help output does not mention %q: %q", want, out)
		}
	}
}
