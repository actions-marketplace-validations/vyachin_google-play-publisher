package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/vyachin/google-play-publisher/internal/cli"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
	"github.com/vyachin/google-play-publisher/internal/version"
)

func TestCompletionCommand(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "completion", "bash")

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}
	if !strings.Contains(out, "gplay") {
		t.Fatalf("completion script does not mention gplay: %q", out)
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

func TestDebugJSONLogFormat(t *testing.T) {
	var out, errOut bytes.Buffer
	app := &cli.App{
		Stdout:  &out,
		Stderr:  &errOut,
		Version: version.Info{Version: "test"},
		NewAPI: func(context.Context, play.Options) (play.API, error) {
			return &playtest.Fake{}, nil
		},
	}

	code := cli.Execute(context.Background(), app, []string{
		"--debug", "--log-format", "json", "publish",
		"-p", "com.example.app", "-f", writeAAB(t),
	})
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut.String())
	}

	line := strings.Split(strings.TrimSpace(errOut.String()), "\n")[0]
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("stderr line is not JSON: %v (%q)", err, line)
	}
	if record["level"] != "DEBUG" && record["level"] != "INFO" {
		t.Fatalf("unexpected log level: %#v", record)
	}
}
