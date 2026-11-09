package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	NotesPath               string
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
		Long:    "Upload a signed Android App Bundle, declare the release on a track and commit the edit.",
		Args:    noArgs,
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
	f.StringVar(&flags.NotesPath, "release-notes", "", "JSON file with localized release notes")
	f.BoolVar(&flags.ChangesNotSentForReview, "changes-not-sent-for-review", false,
		"save changes without sending them for review")
	f.StringVar(&flags.OnInReview, "on-in-review", "error", "behavior when changes are in review: error or replace")
	f.StringVar(&flags.IfExists, "if-exists", "fail", "behavior when versionCode is already released: fail or skip")
	f.BoolVar(&flags.DryRun, "dry-run", false, "validate the edit and delete it without committing")

	return cmd
}

func runPublish(cmd *cobra.Command, a *App, g *globalFlags, flags *publishFlags) error {
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

	notes, err := readReleaseNotes(flags.NotesPath)
	if err != nil {
		return err
	}

	if a.NewAPI == nil {
		return exitcode.Errorf(exitcode.Internal, "API client is not configured")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), g.Timeout)
	defer cancel()

	api, err := a.NewAPI(ctx, play.Options{
		CredentialsFile: g.Credentials,
		Endpoint:        g.Endpoint,
		UserAgent:       "gplay-publisher/" + a.Version.Version,
		Logger:          a.Logger,
		Debug:           g.Debug,
	})
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
		Track:                   flags.Track,
		Status:                  status,
		Rollout:                 flags.Rollout,
		RolloutSet:              cmd.Flags().Changed("rollout"),
		Notes:                   notes,
		IfExists:                exists,
		ChangesNotSentForReview: flags.ChangesNotSentForReview,
		InReviewBehavior:        inReview,
		DryRun:                  flags.DryRun,
	})
	if err != nil {
		return err
	}

	if g.JSON {
		return writePublishJSON(a.Stdout, result, time.Since(start))
	}
	printPublish(a.Stdout, result)
	return nil
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

func readReleaseNotes(path string) (map[string]string, error) {
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the path passed to --release-notes",
		)
	}

	var notes map[string]string
	if err := json.Unmarshal(data, &notes); err != nil {
		return nil, exitcode.Wrap(exitcode.Usage,
			fmt.Errorf("parse %s: %w", path, err))
	}
	return notes, nil
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
	default:
		_, _ = fmt.Fprintf(w, "published %s version %d to %s (%s, review %s)\n",
			result.Package, result.VersionCode, result.Track, result.Status, result.Review)
	}
}
