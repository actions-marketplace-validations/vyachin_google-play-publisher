package app_test

import (
	"context"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

func signedAAB(t *testing.T) string {
	t.Helper()
	return writeBundleFiles(t, map[string]string{
		"BundleConfig.pb":   "bundle-config",
		"META-INF/CERT.RSA": "signature",
	})
}

func TestDoctorSuccess(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return []play.ReleaseSummary{{Name: "1.0"}}, nil
		},
	}

	checks, err := app.Doctor(context.Background(), fake, app.DoctorOptions{
		Package:      "com.example.app",
		AABPath:      signedAAB(t),
		MetadataPath: writeMetadata(t, 42),
	})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	byName := make(map[string]app.Check, len(checks))
	for _, check := range checks {
		byName[check.Name] = check
	}
	for _, name := range []string{"api", "track", "artifact", "signature", "metadata"} {
		check, ok := byName[name]
		if !ok {
			t.Fatalf("missing %q check in %#v", name, checks)
		}
		if !check.OK {
			t.Fatalf("%q check failed: %s", name, check.Detail)
		}
	}
}

func TestDoctorUnsignedAAB(t *testing.T) {
	checks, err := app.Doctor(context.Background(), &playtest.Fake{}, app.DoctorOptions{
		Package: "com.example.app",
		AABPath: writeAAB(t),
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Rejected {
		t.Fatalf("code = %d, want %d", got, exitcode.Rejected)
	}

	last := checks[len(checks)-1]
	if last.Name != "signature" || last.OK {
		t.Fatalf("unexpected last check: %#v", last)
	}
}

func TestDoctorAPIError(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return nil, exitcode.Errorf(exitcode.Auth, "no access")
		},
	}

	checks, err := app.Doctor(context.Background(), fake, app.DoctorOptions{Package: "com.example.app"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Auth {
		t.Fatalf("code = %d, want %d", got, exitcode.Auth)
	}
	if len(checks) != 1 || checks[0].Name != "api" || checks[0].OK {
		t.Fatalf("unexpected checks: %#v", checks)
	}
}

func TestDoctorMetadataMismatch(t *testing.T) {
	_, err := app.Doctor(context.Background(), &playtest.Fake{}, app.DoctorOptions{
		Package:      "com.other.app",
		AABPath:      signedAAB(t),
		MetadataPath: writeMetadata(t, 42),
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Usage {
		t.Fatalf("code = %d, want %d", got, exitcode.Usage)
	}
}

func TestDoctorRequiresPackage(t *testing.T) {
	_, err := app.Doctor(context.Background(), &playtest.Fake{}, app.DoctorOptions{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Usage {
		t.Fatalf("code = %d, want %d", got, exitcode.Usage)
	}
}

func TestStatus(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return []play.ReleaseSummary{{
				Track:        "internal",
				Name:         "1.0",
				State:        "RELEASE_LIFECYCLE_STATE_PUBLISHED",
				VersionCodes: []int64{42},
			}}, nil
		},
	}

	result, err := app.Status(context.Background(), fake, "com.example.app", "internal")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if result.Track != "internal" || len(result.Releases) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Releases[0].VersionCodes[0] != 42 {
		t.Fatalf("unexpected versions: %#v", result.Releases[0].VersionCodes)
	}
}

func TestStatusEmptyReleases(t *testing.T) {
	result, err := app.Status(context.Background(), &playtest.Fake{}, "com.example.app", "")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if result.Track != "internal" {
		t.Errorf("Track = %q, want internal", result.Track)
	}
	if result.Releases == nil {
		t.Fatal("Releases must be an empty slice, not nil")
	}
	if len(result.Releases) != 0 {
		t.Fatalf("Releases = %d, want 0", len(result.Releases))
	}
}

func TestStatusAPIError(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return nil, exitcode.Errorf(exitcode.NotFound, "track not found")
		},
	}

	_, err := app.Status(context.Background(), fake, "com.example.app", "internal")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.NotFound {
		t.Fatalf("code = %d, want %d", got, exitcode.NotFound)
	}
}

func TestStatusRequiresPackage(t *testing.T) {
	_, err := app.Status(context.Background(), &playtest.Fake{}, "", "internal")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Usage {
		t.Fatalf("code = %d, want %d", got, exitcode.Usage)
	}
}
