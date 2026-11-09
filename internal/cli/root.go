// Package cli wires commands, global flags and process exit codes.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	appcore "github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/logging"
)

// globalFlags are the flags every command accepts.
type globalFlags struct {
	Debug       bool
	JSON        bool
	LogFormat   string
	Timeout     time.Duration
	Credentials string
	Endpoint    string
}

// Execute runs the command tree and returns a process exit code.
func Execute(ctx context.Context, app *App, args []string) int {
	cmd, g := newRootCmd(app)
	cmd.SetArgs(args)

	executed, err := cmd.ExecuteContextC(ctx)
	if err == nil {
		return int(exitcode.OK)
	}

	// A command can render its JSON result before returning its error. Keep
	// that fact even when cancellation replaces the error below, so stdout
	// never receives a second object.
	var rendered *renderedError
	alreadyRendered := errors.As(err, &rendered)

	code := exitcode.From(err)
	// A signal replaces the diagnostic with a generic interruption. An
	// unverified commit is the exception: its message names the edit that must
	// be checked, so it is preserved even though the code is already 130.
	if ctx.Err() != nil && !appcore.IsCommitStateUnknown(err) {
		code = exitcode.Interrupted
		err = exitcode.Errorf(exitcode.Interrupted, "operation interrupted")
	}

	if g.JSON {
		if !alreadyRendered {
			if writeErr := writeErrorJSON(app.Stdout, executed, err, code); writeErr != nil {
				printError(app.Stderr, writeErr, exitcode.Internal)
				return int(exitcode.Internal)
			}
		}
		printError(app.Stderr, err, code)
		return int(code)
	}

	printError(app.Stderr, err, code)
	return int(code)
}

// NewRootCmd builds the root command with all subcommands attached.
func NewRootCmd(app *App) *cobra.Command {
	root, _ := newRootCmd(app)
	return root
}

func newRootCmd(app *App) (*cobra.Command, *globalFlags) {
	g := &globalFlags{}

	root := &cobra.Command{
		Use:           "google-play-publisher",
		Short:         "Publish Android App Bundles to Google Play",
		Long:          "google-play-publisher publishes signed Android App Bundles (AAB) to Google Play tracks.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return exitcode.Errorf(exitcode.Usage, "unknown command %q for %q", args[0], cmd.CommandPath())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if !logging.ValidFormat(g.LogFormat) {
				return exitcode.Errorf(exitcode.Usage, "invalid --log-format %q: want %q or %q", g.LogFormat, "text", "json")
			}
			if g.Timeout <= 0 {
				return exitcode.Errorf(exitcode.Usage, "--timeout must be positive, got %s", g.Timeout)
			}
			app.Logger = logging.New(logging.Options{
				Debug:  g.Debug,
				Format: g.LogFormat,
				Out:    app.Stderr,
			})
			return nil
		},
	}

	root.SetOut(app.Stdout)
	root.SetErr(app.Stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitcode.Wrap(exitcode.Usage, err)
	})

	flags := root.PersistentFlags()
	flags.BoolVarP(&g.Debug, "debug", "d", false, "enable detailed debug logging")
	flags.BoolVar(&g.JSON, "json", false, "print machine-readable JSON result")
	flags.StringVar(&g.LogFormat, "log-format", "text", `log format: "text" or "json"`)
	flags.DurationVar(&g.Timeout, "timeout", 20*time.Minute, "overall operation timeout")
	flags.StringVar(&g.Credentials, "credentials", "", "path to service account JSON key (default: ADC)")
	flags.StringVar(&g.Endpoint, "endpoint", "", "override API endpoint (testing)")

	root.AddGroup(
		&cobra.Group{ID: "release", Title: "Release commands:"},
		&cobra.Group{ID: "inspect", Title: "Inspect commands:"},
	)
	root.AddCommand(
		newPublishCmd(app, g),
		newRolloutCmd(app, g),
		newPromoteCmd(app, g),
		newDoctorCmd(app, g),
		newStatusCmd(app, g),
		newVersionCmd(app, g),
	)

	return root, g
}

// printError writes the final error message once, with an optional hint.
func printError(w io.Writer, err error, code exitcode.Code) {
	_, _ = fmt.Fprintf(w, "google-play-publisher: %v\n", err)

	var coded *exitcode.Error
	if errors.As(err, &coded) && coded.Hint != "" {
		_, _ = fmt.Fprintf(w, "  hint: %s\n", coded.Hint)
	}
	if code == exitcode.Usage {
		_, _ = fmt.Fprintln(w, "Run 'google-play-publisher --help' for usage.")
	}
}
