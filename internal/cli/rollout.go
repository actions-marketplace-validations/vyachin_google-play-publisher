package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	appcore "github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/release"
)

type rolloutFlags struct {
	Package                 string
	Track                   string
	VersionCode             int64
	Fraction                float64
	Halt                    bool
	Complete                bool
	ChangesNotSentForReview bool
	OnInReview              string
	DryRun                  bool
}

func newRolloutCmd(a *App, g *globalFlags) *cobra.Command {
	flags := &rolloutFlags{}

	cmd := &cobra.Command{
		Use:     "rollout",
		GroupID: "release",
		Short:   "Change the rollout state of an existing release",
		Long: `Change the user fraction, halt or complete a release that is already on a track,
without uploading a bundle. The command reads the release inside a new edit, changes only its
status and fraction and commits the edit; release notes, name, version codes and in-app update
priority are kept.

Without --version-code the single inProgress or halted release is used. When several releases
match, pass --version-code.`,
		Example: `  # increase a staged rollout to 20% of users
  google-play-publisher rollout -p com.example.app -t production --fraction 0.2

  # stop serving the update
  google-play-publisher rollout -p com.example.app -t production --version-code 42 --halt

  # roll out to everyone
  google-play-publisher rollout -p com.example.app -t production --version-code 42 --complete`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRollout(cmd, a, g, flags)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.Package, "package", "p", "", "application id")
	f.StringVarP(&flags.Track, "track", "t", "internal", "release track")
	f.Int64Var(&flags.VersionCode, "version-code", 0, "version code to change (default: the single inProgress or halted release)")
	f.Float64Var(&flags.Fraction, "fraction", 0, "user fraction between 0 and 1 for an inProgress release")
	f.BoolVar(&flags.Halt, "halt", false, "stop serving the release")
	f.BoolVar(&flags.Complete, "complete", false, "roll the release out to all users")
	f.BoolVar(&flags.ChangesNotSentForReview, "changes-not-sent-for-review", false,
		"save changes without sending them for review")
	f.StringVar(&flags.OnInReview, "on-in-review", "error", "behavior when changes are in review: error or replace")
	f.BoolVar(&flags.DryRun, "dry-run", false, "validate the edit and delete it without committing")

	registerTrackCompletion(cmd)
	return cmd
}

func runRollout(cmd *cobra.Command, a *App, g *globalFlags, flags *rolloutFlags) error {
	inReview, err := parseInReview(flags.OnInReview)
	if err != nil {
		return err
	}

	if cmd.Flags().Changed("version-code") && flags.VersionCode <= 0 {
		return exitcode.Errorf(exitcode.Usage, "--version-code must be positive, got %d", flags.VersionCode)
	}

	var fraction *float64
	if cmd.Flags().Changed("fraction") {
		// Copy the flag value: RolloutOptions must not alias the flag storage.
		value := flags.Fraction
		fraction = &value
	}

	opts := appcore.RolloutOptions{
		Package:                 flags.Package,
		Track:                   flags.Track,
		VersionCode:             flags.VersionCode,
		Fraction:                fraction,
		Halt:                    flags.Halt,
		Complete:                flags.Complete,
		ChangesNotSentForReview: flags.ChangesNotSentForReview,
		InReviewBehavior:        inReview,
		DryRun:                  flags.DryRun,
	}

	// Validate every local input before the client is built, so bad flags
	// return code 2 even when credentials are unavailable.
	prepared, err := appcore.PrepareRollout(opts)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), g.Timeout)
	defer cancel()

	api, err := newAPIClient(ctx, a, g)
	if err != nil {
		return err
	}

	publisher := appcore.NewPublisher(api, a.Logger, appcore.WithRecoveryBase(cmd.Context()))

	start := time.Now()
	result, err := publisher.RolloutPrepared(ctx, prepared)
	if err != nil {
		if exitcode.From(err) == exitcode.ManualAction && result.EditID != "" {
			return reportSavedTrackChange(a, g, rolloutJSON(result), rolloutLine(result), time.Since(start), err)
		}
		return err
	}

	if g.JSON {
		return writeTrackChangeJSON(a.Stdout, rolloutJSON(result), time.Since(start))
	}
	_, err = fmt.Fprintln(a.Stdout, rolloutLine(result))
	return err
}

func rolloutJSON(result appcore.RolloutResult) trackChangeJSON {
	return trackChangeJSON{
		Command:      "rollout",
		Package:      result.Package,
		Track:        result.Track,
		Status:       result.Status,
		VersionCodes: result.VersionCodes,
		UserFraction: result.UserFraction,
		EditID:       result.EditID,
		Review:       result.Review,
	}
}

func rolloutLine(result appcore.RolloutResult) string {
	codes := formatVersionCodes(result.VersionCodes)

	switch {
	case result.Review == appcore.ReviewValidated:
		return fmt.Sprintf("validated rollout for %s version %s on %s (dry run, edit %s deleted)",
			result.Package, codes, result.Track, result.EditID)
	case result.Review == appcore.ReviewNotSent:
		return fmt.Sprintf("saved rollout for %s version %s on %s at %s without review (edit %s)",
			result.Package, codes, result.Track, formatPercent(result.UserFraction), result.EditID)
	case result.Status == string(release.StatusHalted):
		return fmt.Sprintf("halted %s version %s on %s (edit %s)",
			result.Package, codes, result.Track, result.EditID)
	case result.Status == string(release.StatusCompleted):
		return fmt.Sprintf("completed %s version %s on %s (edit %s)",
			result.Package, codes, result.Track, result.EditID)
	default:
		return fmt.Sprintf("rollout for %s version %s on %s is at %s (edit %s)",
			result.Package, codes, result.Track, formatPercent(result.UserFraction), result.EditID)
	}
}
