package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

func newVersionCmd(app *App, g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print version information",
		Long:    "Print google-play-publisher version, commit, build date and the Go version used to build the binary.",
		Example: "  google-play-publisher version\n  google-play-publisher --json version",
		Args:    noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if g.JSON {
				enc := json.NewEncoder(app.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(app.Version); err != nil {
					return exitcode.Wrap(exitcode.Internal, err)
				}
				return nil
			}

			_, _ = fmt.Fprintf(app.Stdout, "google-play-publisher version %s\n", app.Version)
			return nil
		},
	}
}

// noArgs rejects positional arguments with a usage exit code.
func noArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return exitcode.Wrap(exitcode.Usage, err)
	}
	return nil
}
