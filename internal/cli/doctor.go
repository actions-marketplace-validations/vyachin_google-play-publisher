package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	appcore "github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

type doctorFlags struct {
	Package  string
	AAB      string
	Metadata string
	Track    string
}

type doctorCheckOutput struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type doctorOutput struct {
	OK      bool                `json:"ok"`
	Command string              `json:"command"`
	Package string              `json:"package"`
	Checks  []doctorCheckOutput `json:"checks"`
	Error   *errorDetail        `json:"error,omitempty"`
}

func newDoctorCmd(a *App, g *globalFlags) *cobra.Command {
	flags := &doctorFlags{}

	cmd := &cobra.Command{
		Use:     "doctor",
		GroupID: "inspect",
		Short:   "Check Play access, track and AAB before publishing",
		Long:    "Run read-only checks: API access, target track, AAB shape, signature and AGP metadata.",
		Example: `  google-play-publisher doctor -p com.example.app
  google-play-publisher doctor -p com.example.app -f app-release.aab --metadata output-metadata.json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd, a, g, flags)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.Package, "package", "p", "", "application id")
	f.StringVarP(&flags.AAB, "aab", "f", "", "path to a signed .aab file")
	f.StringVar(&flags.Metadata, "metadata", "", "path to AGP output-metadata.json")
	f.StringVarP(&flags.Track, "track", "t", "internal", "release track")

	registerTrackCompletion(cmd)
	return cmd
}

func runDoctor(cmd *cobra.Command, a *App, g *globalFlags, flags *doctorFlags) error {
	if flags.Package == "" {
		return exitcode.Errorf(exitcode.Usage, "--package is required")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), g.Timeout)
	defer cancel()

	api, err := newAPIClient(ctx, a, g)
	if err != nil {
		return err
	}

	checks, checkErr := appcore.Doctor(ctx, api, appcore.DoctorOptions{
		Package:      flags.Package,
		AABPath:      flags.AAB,
		MetadataPath: flags.Metadata,
		Track:        flags.Track,
	})

	if g.JSON {
		if err := writeDoctorJSON(a.Stdout, flags.Package, checks, checkErr); err != nil {
			return err
		}
		if checkErr != nil {
			return &renderedError{err: checkErr}
		}
		return nil
	}

	printChecks(a.Stdout, checks)
	return checkErr
}

func writeDoctorJSON(w io.Writer, pkg string, checks []appcore.Check, checkErr error) error {
	out := doctorOutput{
		OK:      checkErr == nil,
		Command: "doctor",
		Package: pkg,
		Checks:  make([]doctorCheckOutput, 0, len(checks)),
	}
	for _, check := range checks {
		out.Checks = append(out.Checks, doctorCheckOutput{Name: check.Name, OK: check.OK, Detail: check.Detail})
	}
	if checkErr != nil {
		detail := newErrorDetail(checkErr, exitcode.From(checkErr))
		out.Error = &detail
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return exitcode.Wrap(exitcode.Internal, err)
	}
	return nil
}

func printChecks(w io.Writer, checks []appcore.Check) {
	for _, check := range checks {
		status := "ok  "
		if !check.OK {
			status = "FAIL"
		}
		_, _ = fmt.Fprintf(w, "%s %-10s %s\n", status, check.Name+":", check.Detail)
	}
}
