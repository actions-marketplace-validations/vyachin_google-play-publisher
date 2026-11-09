package cli

import (
	"github.com/spf13/cobra"
)

// registerTrackCompletion suggests common track names for --track.
func registerTrackCompletion(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("track",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return []string{"internal", "alpha", "beta", "production"}, cobra.ShellCompDirectiveNoFileComp
		})
}
