package cli

import (
	"github.com/spf13/cobra"
)

// registerTrackCompletion suggests common track names for --track.
func registerTrackCompletion(cmd *cobra.Command) {
	registerTracksCompletion(cmd, "track")
}

// registerTracksCompletion suggests common track names for the named flags,
// for example --from and --to of promote.
func registerTracksCompletion(cmd *cobra.Command, names ...string) {
	suggest := func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"internal", "alpha", "beta", "production"}, cobra.ShellCompDirectiveNoFileComp
	}
	for _, name := range names {
		_ = cmd.RegisterFlagCompletionFunc(name, suggest)
	}
}
