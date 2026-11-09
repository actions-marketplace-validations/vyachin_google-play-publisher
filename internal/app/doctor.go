package app

import (
	"context"
	"fmt"

	"github.com/vyachin/google-play-publisher/internal/artifact"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
)

// Check is one diagnostic result produced by Doctor.
type Check struct {
	Name   string
	OK     bool
	Detail string
}

// DoctorOptions describe a doctor run.
type DoctorOptions struct {
	Package      string
	AABPath      string
	MetadataPath string
	Track        string
}

// Doctor verifies Play access, the target track and the local AAB.
func Doctor(ctx context.Context, api play.API, opts DoctorOptions) ([]Check, error) {
	if opts.Package == "" {
		return nil, exitcode.Errorf(exitcode.Usage, "--package is required")
	}

	track := opts.Track
	if track == "" {
		track = "internal"
	}

	checks := make([]Check, 0, 6)

	releases, err := api.TrackReleases(ctx, opts.Package, track)
	if err != nil {
		checks = append(checks, Check{Name: "api", Detail: err.Error()})
		return checks, err
	}
	checks = append(checks, Check{Name: "api", OK: true, Detail: "authenticated; application is accessible"})
	checks = append(checks, Check{
		Name:   "track",
		OK:     true,
		Detail: fmt.Sprintf("%d release(s) on %s", len(releases), track),
	})

	if opts.AABPath == "" {
		return checks, nil
	}

	bundle, err := artifact.Open(opts.AABPath)
	if err != nil {
		checks = append(checks, Check{Name: "artifact", Detail: err.Error()})
		return checks, err
	}
	defer func() { _ = bundle.Close() }()

	info := bundle.Info
	checks = append(checks, Check{
		Name:   "artifact",
		OK:     true,
		Detail: fmt.Sprintf("%d bytes, sha256 %s", info.Size, info.SHA256),
	})

	if !info.Signed {
		checks = append(checks, Check{Name: "signature", Detail: "META-INF signature not found"})
		return checks, exitcode.WithHint(
			exitcode.Errorf(exitcode.Rejected, "AAB has no JAR signature"),
			"sign the bundle with the upload key",
		)
	}
	checks = append(checks, Check{Name: "signature", OK: true, Detail: "JAR signature present"})

	// Doctor checks the same manifest identity as publish: a bundle for
	// another application must not be reported as ready for this one.
	resolved, err := validateIdentity(bundle.Identity, opts.Package, 0, nil)
	if err != nil {
		checks = append(checks, Check{Name: "identity", Detail: err.Error()})
		return checks, err
	}
	checks = append(checks, Check{
		Name: "identity",
		OK:   true,
		Detail: fmt.Sprintf("applicationId %s, versionCode %d, versionName %s",
			resolved.Package, resolved.VersionCode, resolved.VersionName),
	})

	if opts.MetadataPath != "" {
		meta, err := artifact.ReadMetadata(opts.MetadataPath)
		if err != nil {
			checks = append(checks, Check{Name: "metadata", Detail: err.Error()})
			return checks, err
		}
		if _, err := validateIdentity(bundle.Identity, opts.Package, 0, &meta); err != nil {
			checks = append(checks, Check{Name: "metadata", Detail: err.Error()})
			return checks, err
		}
		checks = append(checks, Check{
			Name:   "metadata",
			OK:     true,
			Detail: fmt.Sprintf("applicationId %s, versionCode %d", meta.ApplicationID, meta.VersionCode),
		})
	}

	return checks, nil
}
