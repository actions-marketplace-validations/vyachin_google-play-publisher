package cli_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vyachin/google-play-publisher/internal/cli"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
	"github.com/vyachin/google-play-publisher/internal/version"
)

func TestCompletionCommand(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "completion", "bash")

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}
	if !strings.Contains(out, "google-play-publisher") {
		t.Fatalf("completion script does not mention google-play-publisher: %q", out)
	}
}

func TestTrackCompletion(t *testing.T) {
	app := &cli.App{
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Version: version.Info{Version: "test"},
	}
	root := cli.NewRootCmd(app)

	cmd, _, err := root.Find([]string{"publish"})
	if err != nil {
		t.Fatalf("find publish: %v", err)
	}

	complete, ok := cmd.GetFlagCompletionFunc("track")
	if !ok {
		t.Fatal("track flag completion is not registered")
	}

	suggestions, directive := complete(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}

	want := map[string]bool{"internal": false, "alpha": false, "beta": false, "production": false}
	for _, suggestion := range suggestions {
		if _, exists := want[suggestion]; exists {
			want[suggestion] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("suggestion %q missing from %v", name, suggestions)
		}
	}
}

func TestCommandExamples(t *testing.T) {
	for _, command := range []string{"publish", "doctor", "status"} {
		t.Run(command, func(t *testing.T) {
			out, _, code := runWithAPI(t, &playtest.Fake{}, command, "--help")
			if code != int(exitcode.OK) {
				t.Fatalf("code = %d, want %d", code, exitcode.OK)
			}
			if !strings.Contains(out, "Examples:") {
				t.Fatalf("%s help has no examples: %q", command, out)
			}
		})
	}
}

func TestDebugKeepsJSONStdoutClean(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "--json", "--debug", "publish",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v (%q)", err, out)
	}
	if !strings.Contains(errOut, "level=DEBUG") {
		t.Fatalf("debug logs are missing from stderr: %q", errOut)
	}
	if strings.Contains(out, "level=") {
		t.Fatalf("logs leaked into stdout: %q", out)
	}
}

func TestDebugLogsHaveNoSecrets(t *testing.T) {
	_, errOut, code := runWithAPI(t, &playtest.Fake{}, "--debug", "publish",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d", code, exitcode.OK)
	}
	if !strings.Contains(errOut, "level=") {
		t.Fatalf("debug logs are missing: %q", errOut)
	}
	for _, secret := range []string{"Authorization", "private_key", "Bearer ", "ya29."} {
		if strings.Contains(errOut, secret) {
			t.Fatalf("debug log leaks %q: %q", secret, errOut)
		}
	}
}
