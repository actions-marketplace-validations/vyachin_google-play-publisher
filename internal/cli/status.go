package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	appcore "github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

type statusFlags struct {
	Package string
	Track   string
}

type statusReleaseOutput struct {
	Name         string  `json:"name"`
	State        string  `json:"state"`
	VersionCodes []int64 `json:"versionCodes"`
}

type statusOutput struct {
	OK       bool                  `json:"ok"`
	Command  string                `json:"command"`
	Package  string                `json:"package"`
	Track    string                `json:"track"`
	Releases []statusReleaseOutput `json:"releases"`
}

func newStatusCmd(a *App, g *globalFlags) *cobra.Command {
	flags := &statusFlags{}

	cmd := &cobra.Command{
		Use:     "status",
		GroupID: "inspect",
		Short:   "Show releases on a track",
		Long:    "Show releases on a track without creating an edit. Uses the read-only releases API.",
		Example: `  gplay status -p com.example.app
  gplay status -p com.example.app -t production --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd, a, g, flags)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.Package, "package", "p", "", "application id")
	f.StringVarP(&flags.Track, "track", "t", "internal", "release track")

	registerTrackCompletion(cmd)
	return cmd
}

func runStatus(cmd *cobra.Command, a *App, g *globalFlags, flags *statusFlags) error {
	if flags.Package == "" {
		return exitcode.Errorf(exitcode.Usage, "--package is required")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), g.Timeout)
	defer cancel()

	api, err := newAPIClient(ctx, a, g)
	if err != nil {
		return err
	}

	result, err := appcore.Status(ctx, api, flags.Package, flags.Track)
	if err != nil {
		return err
	}

	if g.JSON {
		return writeStatusJSON(a.Stdout, result)
	}
	printStatus(a.Stdout, result)
	return nil
}

func writeStatusJSON(w io.Writer, result appcore.StatusResult) error {
	out := statusOutput{
		OK:       true,
		Command:  "status",
		Package:  result.Package,
		Track:    result.Track,
		Releases: make([]statusReleaseOutput, 0, len(result.Releases)),
	}
	for _, rel := range result.Releases {
		versions := make([]int64, len(rel.VersionCodes))
		copy(versions, rel.VersionCodes)
		out.Releases = append(out.Releases, statusReleaseOutput{
			Name:         rel.Name,
			State:        rel.State,
			VersionCodes: versions,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return exitcode.Wrap(exitcode.Internal, err)
	}
	return nil
}

func printStatus(w io.Writer, result appcore.StatusResult) {
	if len(result.Releases) == 0 {
		_, _ = fmt.Fprintf(w, "no releases on %s\n", result.Track)
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tSTATE\tVERSION CODES")
	for _, rel := range result.Releases {
		name := rel.Name
		if name == "" {
			name = "-"
		}
		codes := make([]string, 0, len(rel.VersionCodes))
		for _, code := range rel.VersionCodes {
			codes = append(codes, strconv.FormatInt(code, 10))
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", name, rel.State, strings.Join(codes, ","))
	}
	_ = tw.Flush()
}
