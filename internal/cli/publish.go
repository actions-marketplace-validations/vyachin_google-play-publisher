package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	appcore "github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/release"
)

type publishFlags struct {
	Package                 string
	AAB                     string
	Metadata                string
	Track                   string
	Status                  string
	Rollout                 float64
	ChangelogDir            string
	VersionCode             int64
	ReleaseName             string
	ChangesNotSentForReview bool
	OnInReview              string
	IfExists                string
	DryRun                  bool
}

type publishOutput struct {
	OK                bool   `json:"ok"`
	Command           string `json:"command"`
	Package           string `json:"package"`
	Track             string `json:"track"`
	Status            string `json:"status"`
	VersionCode       int64  `json:"versionCode"`
	SHA256            string `json:"sha256"`
	EditID            string `json:"editId,omitempty"`
	Review            string `json:"review"`
	ManagedPublishing string `json:"managedPublishing"`
	DurationMs        int64  `json:"durationMs"`
}

func newPublishCmd(a *App, g *globalFlags) *cobra.Command {
	flags := &publishFlags{}

	cmd := &cobra.Command{
		Use:     "publish",
		GroupID: "release",
		Short:   "Upload an AAB and publish it to a track",
		Long: `Upload a signed Android App Bundle, declare the release on a track and commit
the edit. The default track is "internal".`,
		Example: `  # publish to the internal track
  google-play-publisher publish -p com.example.app -f app-release.aab -t internal

  # staged rollout on production
  google-play-publisher publish -p com.example.app -f app-release.aab -t production --rollout 0.1

  # validate without committing
  google-play-publisher publish -p com.example.app -f app-release.aab --dry-run`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPublish(cmd, a, g, flags)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.Package, "package", "p", "", "application id")
	f.StringVarP(&flags.AAB, "aab", "f", "", "path to a signed .aab file")
	f.StringVar(&flags.Metadata, "metadata", "", "path to AGP output-metadata.json")
	f.StringVarP(&flags.Track, "track", "t", "internal", "release track")
	f.StringVar(&flags.Status, "status", "completed", "release status: draft, inProgress or completed")
	f.Float64Var(&flags.Rollout, "rollout", 0, "staged rollout fraction between 0 and 1")
	f.StringVar(&flags.ChangelogDir, "changelog-dir", "", "directory with <locale>/changelogs/<versionCode>.txt release notes")
	f.Int64Var(&flags.VersionCode, "version-code", 0, "version code used to look up changelog files and verify the upload")
	f.StringVar(&flags.ReleaseName, "release-name", "", "release name shown in Play Console")
	f.BoolVar(&flags.ChangesNotSentForReview, "changes-not-sent-for-review", false,
		"save changes without sending them for review")
	f.StringVar(&flags.OnInReview, "on-in-review", "error", "behavior when changes are in review: error or replace")
	f.StringVar(&flags.IfExists, "if-exists", "fail", "behavior when versionCode is already released: fail or skip")
	f.BoolVar(&flags.DryRun, "dry-run", false, "validate the edit and delete it without committing")

	registerTrackCompletion(cmd)
	return cmd
}

func runPublish(cmd *cobra.Command, a *App, g *globalFlags, flags *publishFlags) error {
	if flags.Package == "" && flags.Metadata != "" {
		pkg, err := appcore.PackageFromMetadata(flags.Metadata)
		if err != nil {
			return err
		}
		if err := cmd.Flags().Set("package", pkg); err != nil {
			return exitcode.Wrap(exitcode.Usage, err)
		}
	}
	if flags.Package == "" {
		return exitcode.Errorf(exitcode.Usage, "--package is required")
	}
	if flags.AAB == "" {
		return exitcode.Errorf(exitcode.Usage, "--aab is required")
	}
	if flags.ChangesNotSentForReview && flags.DryRun {
		return exitcode.Errorf(exitcode.Usage,
			"--changes-not-sent-for-review cannot be combined with --dry-run")
	}

	inReview, err := parseInReview(flags.OnInReview)
	if err != nil {
		return err
	}

	exists, err := release.ParseExistsPolicy(flags.IfExists)
	if err != nil {
		return err
	}

	if cmd.Flags().Changed("version-code") && flags.VersionCode <= 0 {
		return exitcode.Errorf(exitcode.Usage, "--version-code must be positive, got %d", flags.VersionCode)
	}
	if flags.ChangelogDir != "" && flags.VersionCode <= 0 {
		return exitcode.Errorf(exitcode.Usage, "--version-code is required with --changelog-dir")
	}

	notes, err := readChangelogDir(flags.ChangelogDir, flags.VersionCode)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), g.Timeout)
	defer cancel()

	api, err := newAPIClient(ctx, a, g)
	if err != nil {
		return err
	}

	publisher := appcore.NewPublisher(api, a.Logger)

	status := flags.Status
	if !cmd.Flags().Changed("status") {
		status = "" // let --rollout imply inProgress
	}

	start := time.Now()
	result, err := publisher.Publish(ctx, appcore.PublishOptions{
		Package:                 flags.Package,
		AABPath:                 flags.AAB,
		MetadataPath:            flags.Metadata,
		VersionCode:             flags.VersionCode,
		Track:                   flags.Track,
		Status:                  status,
		Rollout:                 flags.Rollout,
		RolloutSet:              cmd.Flags().Changed("rollout"),
		Notes:                   notes,
		ReleaseName:             flags.ReleaseName,
		IfExists:                exists,
		ChangesNotSentForReview: flags.ChangesNotSentForReview,
		InReviewBehavior:        inReview,
		DryRun:                  flags.DryRun,
	})
	if err != nil {
		// A manual-review fallback saves the release and still needs exit
		// code 8, so the saved result must be reported before the error.
		if exitcode.From(err) == exitcode.ManualAction && result.VersionCode != 0 {
			return reportManualAction(a, g, result, time.Since(start), err)
		}
		return err
	}

	if g.JSON {
		return writePublishJSON(a.Stdout, result, time.Since(start))
	}
	printPublish(a.Stdout, result)
	return nil
}

// reportManualAction prints the saved release and keeps the manual-action exit
// code. In JSON mode the success object is the only object on stdout.
func reportManualAction(a *App, g *globalFlags, result appcore.PublishResult, duration time.Duration, err error) error {
	if g.JSON {
		if writeErr := writePublishJSON(a.Stdout, result, duration); writeErr != nil {
			return writeErr
		}
		return &renderedError{err: err}
	}

	printPublish(a.Stdout, result)
	return err
}

func parseInReview(raw string) (string, error) {
	switch raw {
	case "", "error":
		return play.InReviewError, nil
	case "replace":
		return play.InReviewReplace, nil
	default:
		return "", exitcode.Errorf(exitcode.Usage,
			"invalid --on-in-review %q: want error or replace", raw)
	}
}

func writePublishJSON(w io.Writer, result appcore.PublishResult, duration time.Duration) error {
	out := publishOutput{
		OK:                true,
		Command:           "publish",
		Package:           result.Package,
		Track:             result.Track,
		Status:            result.Status,
		VersionCode:       result.VersionCode,
		SHA256:            result.SHA256,
		EditID:            result.EditID,
		Review:            result.Review,
		ManagedPublishing: "unknown",
		DurationMs:        duration.Milliseconds(),
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return exitcode.Wrap(exitcode.Internal, err)
	}
	return nil
}

func printPublish(w io.Writer, result appcore.PublishResult) {
	switch result.Review {
	case appcore.ReviewSkipped:
		_, _ = fmt.Fprintf(w, "skipped: version %d of %s is already on %s\n",
			result.VersionCode, result.Package, result.Track)
	case appcore.ReviewValidated:
		_, _ = fmt.Fprintf(w, "validated %s version %d for %s (dry run, edit %s deleted)\n",
			result.Package, result.VersionCode, result.Track, result.EditID)
	case appcore.ReviewNotSent:
		_, _ = fmt.Fprintf(w, "saved %s version %d to %s without sending it for review (edit %s)\n",
			result.Package, result.VersionCode, result.Track, result.EditID)
	default:
		_, _ = fmt.Fprintf(w, "published %s version %d to %s (%s, review %s)\n",
			result.Package, result.VersionCode, result.Track, result.Status, result.Review)
	}
}
