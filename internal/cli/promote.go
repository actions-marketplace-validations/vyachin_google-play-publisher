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

type promoteFlags struct {
	Package                 string
	From                    string
	To                      string
	VersionCode             int64
	Status                  string
	Rollout                 float64
	ChangelogDir            string
	ReleaseName             string
	InAppUpdatePriority     int
	Countries               []string
	IncludeRestOfWorld      bool
	Yes                     bool
	ChangesNotSentForReview bool
	OnInReview              string
	DryRun                  bool
}

func newPromoteCmd(a *App, g *globalFlags) *cobra.Command {
	flags := &promoteFlags{}

	cmd := &cobra.Command{
		Use:     "promote",
		GroupID: "release",
		Short:   "Promote a release from one track to another",
		Long: `Declare the version codes of a release on one track as a new release on another
track, without uploading a bundle again. The source release is chosen by --version-code or,
when it is omitted, as the release with the highest version code that is not a draft.

Promotion to the production track needs --yes. Use --rollout to start a staged rollout instead
of a full release; --changelog-dir requires --version-code because there is no AAB to read it
from.`,
		Example: `  # promote the latest internal build to closed testing
  google-play-publisher promote -p com.example.app --from internal --to beta

  # staged production rollout of a known version, with notes
  google-play-publisher promote -p com.example.app --from beta --to production \
    --version-code 42 --rollout 0.1 --changelog-dir metadata --yes`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPromote(cmd, a, g, flags)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.Package, "package", "p", "", "application id")
	f.StringVar(&flags.From, "from", "", "source track")
	f.StringVar(&flags.To, "to", "", "destination track")
	f.Int64Var(&flags.VersionCode, "version-code", 0, "version code to promote (default: the highest on the source track)")
	f.StringVar(&flags.Status, "status", "completed", "release status: draft, inProgress or completed")
	f.Float64Var(&flags.Rollout, "rollout", 0, "staged rollout fraction between 0 and 1")
	f.StringVar(&flags.ChangelogDir, "changelog-dir", "",
		"directory with <locale>/changelogs/<versionCode>.txt release notes; requires --version-code")
	f.StringVar(&flags.ReleaseName, "release-name", "", "release name shown in Play Console (default: the source release name)")
	f.IntVar(&flags.InAppUpdatePriority, "in-app-update-priority", 0,
		"in-app update priority from 0 (disabled) to 5 (highest)")
	f.StringSliceVar(&flags.Countries, "country", nil,
		"restrict an inProgress production release to these CLDR country codes (repeatable)")
	f.BoolVar(&flags.IncludeRestOfWorld, "include-rest-of-world", false,
		"include the rest of the world in --country targeting")
	f.BoolVar(&flags.Yes, "yes", false, "confirm a promotion to the production track")
	f.BoolVar(&flags.ChangesNotSentForReview, "changes-not-sent-for-review", false,
		"save changes without sending them for review")
	f.StringVar(&flags.OnInReview, "on-in-review", "error", "behavior when changes are in review: error or replace")
	f.BoolVar(&flags.DryRun, "dry-run", false, "validate the edit and delete it without committing")

	registerTracksCompletion(cmd, "from", "to")
	return cmd
}

func runPromote(cmd *cobra.Command, a *App, g *globalFlags, flags *promoteFlags) error {
	inReview, err := parseInReview(flags.OnInReview)
	if err != nil {
		return err
	}

	if cmd.Flags().Changed("version-code") && flags.VersionCode <= 0 {
		return exitcode.Errorf(exitcode.Usage, "--version-code must be positive, got %d", flags.VersionCode)
	}

	status := flags.Status
	if !cmd.Flags().Changed("status") {
		status = "" // let --rollout imply inProgress
	}

	var rollout *float64
	if cmd.Flags().Changed("rollout") {
		// Copy the flag value: PromoteOptions must not alias the flag storage.
		value := flags.Rollout
		rollout = &value
	}

	opts := appcore.PromoteOptions{
		Package:                 flags.Package,
		From:                    flags.From,
		To:                      flags.To,
		VersionCode:             flags.VersionCode,
		Status:                  status,
		Rollout:                 rollout,
		ChangelogDir:            flags.ChangelogDir,
		ReleaseName:             flags.ReleaseName,
		InAppUpdatePriority:     flags.InAppUpdatePriority,
		Countries:               flags.Countries,
		IncludeRestOfWorld:      flags.IncludeRestOfWorld,
		Confirm:                 flags.Yes,
		ChangesNotSentForReview: flags.ChangesNotSentForReview,
		InReviewBehavior:        inReview,
		DryRun:                  flags.DryRun,
	}

	// Validate every local input before the client is built, so bad flags
	// return code 2 even when credentials are unavailable.
	prepared, err := appcore.PreparePromote(opts)
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
	result, err := publisher.PromotePrepared(ctx, prepared)
	if err != nil {
		if exitcode.From(err) == exitcode.ManualAction && result.EditID != "" {
			return reportSavedTrackChange(a, g, promoteJSON(result), promoteLine(result), time.Since(start), err)
		}
		return err
	}

	if g.JSON {
		return writeTrackChangeJSON(a.Stdout, promoteJSON(result), time.Since(start))
	}
	_, err = fmt.Fprintln(a.Stdout, promoteLine(result))
	return err
}

func promoteJSON(result appcore.PromoteResult) trackChangeJSON {
	return trackChangeJSON{
		Command:      "promote",
		Package:      result.Package,
		From:         result.From,
		Track:        result.To,
		Status:       result.Status,
		VersionCodes: result.VersionCodes,
		UserFraction: result.UserFraction,
		EditID:       result.EditID,
		Review:       result.Review,
	}
}

func promoteLine(result appcore.PromoteResult) string {
	codes := formatVersionCodes(result.VersionCodes)

	switch result.Review {
	case appcore.ReviewValidated:
		return fmt.Sprintf("validated promotion of %s version %s from %s to %s (dry run, edit %s deleted)",
			result.Package, codes, result.From, result.To, result.EditID)
	case appcore.ReviewNotSent:
		return fmt.Sprintf("saved promotion of %s version %s from %s to %s without review (edit %s)",
			result.Package, codes, result.From, result.To, result.EditID)
	}

	detail := result.Status
	if result.Status == string(release.StatusInProgress) {
		detail += " at " + formatPercent(result.UserFraction)
	}
	return fmt.Sprintf("promoted %s version %s from %s to %s (%s, review %s, edit %s)",
		result.Package, codes, result.From, result.To, detail, result.Review, result.EditID)
}
