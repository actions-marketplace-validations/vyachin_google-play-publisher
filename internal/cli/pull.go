package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	appcore "github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

type pullFlags struct {
	Package     string
	MetadataDir string
	Track       string
	VersionCode int64
	Force       bool
}

type pullOutput struct {
	SchemaVersion int      `json:"schemaVersion"`
	OK            bool     `json:"ok"`
	Command       string   `json:"command"`
	Package       string   `json:"package"`
	Tracks        []string `json:"tracks"`
	MetadataDir   string   `json:"metadataDir"`
	VersionCodes  []int64  `json:"versionCodes"`
	Listings      int      `json:"listings"`
	Images        int      `json:"images"`
	Changelogs    int      `json:"changelogs"`
	Testers       int      `json:"testers"`
	DurationMs    int64    `json:"durationMs"`
}

func newPullCmd(a *App, g *globalFlags) *cobra.Command {
	flags := &pullFlags{}

	cmd := &cobra.Command{
		Use:     "pull",
		Aliases: []string{"download"},
		GroupID: "inspect",
		Short:   "Download store metadata into a Play-style directory",
		Long: `Download store listings, images, tester groups and release notes from
Google Play and recreate a metadata directory. The tree matches the layout accepted by
publish --metadata-dir, so it can be committed to the repository and published later.

Pull opens a read-only edit, never commits it, and deletes it before returning. Without
--track every track contributes release notes; without --version-code every release on each
selected track contributes. The target directory must be absent or empty, and --force replaces
a non-empty tree after validation succeeded.`,
		Example: `  # recreate metadata from Play into a new directory
  google-play-publisher pull -p com.example.app --metadata-dir metadata

  # replace an existing metadata tree
  google-play-publisher pull -p com.example.app --metadata-dir metadata --force

  # restrict release notes to one track
  google-play-publisher pull -p com.example.app --metadata-dir metadata -t beta

  # pull one version code only
  google-play-publisher pull -p com.example.app --metadata-dir metadata --version-code 42`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPull(cmd, a, g, flags)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.Package, "package", "p", "", "application id")
	f.StringVar(&flags.MetadataDir, "metadata-dir", "", "target directory for the metadata tree")
	f.StringVarP(&flags.Track, "track", "t", "", "restrict release notes to this track (default: all tracks)")
	f.Int64Var(&flags.VersionCode, "version-code", 0, "pull release notes for this version only (default: every release)")
	f.BoolVar(&flags.Force, "force", false, "replace a non-empty metadata directory")

	registerTrackCompletion(cmd)
	return cmd
}

func runPull(cmd *cobra.Command, a *App, g *globalFlags, flags *pullFlags) error {
	opts := appcore.PullOptions{
		Package:     flags.Package,
		MetadataDir: flags.MetadataDir,
		Track:       flags.Track,
		VersionCode: flags.VersionCode,
		Force:       flags.Force,
	}
	// Fail before the client is built: bad options must return code 2 even
	// when credentials are unavailable. Pull validates again for direct app
	// callers; the rules and messages live in one place.
	if err := appcore.ValidatePullOptions(opts); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), g.Timeout)
	defer cancel()

	api, err := newAPIClient(ctx, a, g)
	if err != nil {
		return err
	}

	start := time.Now()
	result, err := appcore.Pull(ctx, api, opts)
	if err != nil {
		return err
	}

	if g.JSON {
		return writePullJSON(a.Stdout, result, time.Since(start))
	}
	return printPull(a.Stdout, result)
}

func writePullJSON(w io.Writer, result appcore.PullResult, duration time.Duration) error {
	codes := result.VersionCodes
	if codes == nil {
		codes = []int64{}
	}
	tracks := result.Tracks
	if tracks == nil {
		tracks = []string{}
	}
	out := pullOutput{
		SchemaVersion: 1,
		OK:            true,
		Command:       "pull",
		Package:       result.Package,
		Tracks:        tracks,
		MetadataDir:   result.MetadataDir,
		VersionCodes:  codes,
		Listings:      result.Listings,
		Images:        result.Images,
		Changelogs:    result.Changelogs,
		Testers:       result.Testers,
		DurationMs:    duration.Milliseconds(),
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return exitcode.Wrap(exitcode.Internal, err)
	}
	return nil
}

func printPull(w io.Writer, result appcore.PullResult) error {
	if _, err := fmt.Fprintf(w, "pulled %s into %s\n", result.Package, result.MetadataDir); err != nil {
		return err
	}
	lines := []string{
		fmt.Sprintf("  listings: %d", result.Listings),
		fmt.Sprintf("  images: %d", result.Images),
		fmt.Sprintf("  changelogs: %d", result.Changelogs),
		fmt.Sprintf("  testers: %d", result.Testers),
	}
	if len(result.Tracks) == 0 {
		lines = append(lines, "  tracks: none (no releases)")
	} else {
		codes := make([]string, 0, len(result.VersionCodes))
		for _, code := range result.VersionCodes {
			codes = append(codes, strconv.FormatInt(code, 10))
		}
		lines = append(lines, fmt.Sprintf("  tracks: %s (version codes %s)",
			strings.Join(result.Tracks, ", "), strings.Join(codes, ", ")))
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}
