package cli_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/artifact/artifacttest"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
	"github.com/vyachin/google-play-publisher/internal/play"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

func signedAAB(t *testing.T) string {
	t.Helper()
	return writeBundleFiles(t, map[string]string{
		"BundleConfig.pb":                   "bundle-config",
		"META-INF/CERT.RSA":                 "signature",
		"base/manifest/AndroidManifest.xml": string(artifacttest.Manifest("com.example.app", 42, "1.0.42")),
	})
}

func TestDoctorJSON(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "--json", "doctor",
		"-p", "com.example.app", "-f", signedAAB(t))

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got struct {
		OK      bool `json:"ok"`
		Package string
		Checks  []struct {
			Name string
			OK   bool
		}
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if !got.OK || got.Package != "com.example.app" {
		t.Fatalf("unexpected JSON: %#v", got)
	}
	if len(got.Checks) != 5 {
		t.Fatalf("checks = %d, want 5: %s", len(got.Checks), out)
	}
	for _, check := range got.Checks {
		if !check.OK {
			t.Fatalf("check %q failed: %s", check.Name, out)
		}
	}
}

func TestDoctorHumanOutput(t *testing.T) {
	out, errOut, code := runWithAPI(t, &playtest.Fake{}, "doctor",
		"-p", "com.example.app", "-f", signedAAB(t))

	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}
	if !strings.Contains(out, "ok   api:") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestDoctorUnsignedAAB(t *testing.T) {
	_, errOut, code := runWithAPI(t, &playtest.Fake{}, "doctor",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.Rejected) {
		t.Fatalf("code = %d, want %d", code, exitcode.Rejected)
	}
	if !strings.Contains(errOut, "JAR signature") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
}

func TestDoctorAPIError(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return nil, exitcode.Errorf(exitcode.Auth, "permission denied")
		},
	}

	_, _, code := runWithAPI(t, fake, "doctor", "-p", "com.example.app")
	if code != int(exitcode.Auth) {
		t.Fatalf("code = %d, want %d", code, exitcode.Auth)
	}
}

func TestDoctorRequiresPackage(t *testing.T) {
	_, _, code := runWithAPI(t, &playtest.Fake{}, "doctor")
	if code != int(exitcode.Usage) {
		t.Fatalf("code = %d, want %d", code, exitcode.Usage)
	}
}

func TestStatusJSON(t *testing.T) {
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

	out, errOut, code := runWithAPI(t, fake, "--json", "status", "-p", "com.example.app")
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}

	var got struct {
		OK       bool   `json:"ok"`
		Track    string `json:"track"`
		Releases []struct {
			Name         string  `json:"name"`
			State        string  `json:"state"`
			VersionCodes []int64 `json:"versionCodes"`
		} `json:"releases"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if !got.OK || got.Track != "internal" || len(got.Releases) != 1 {
		t.Fatalf("unexpected JSON: %#v", got)
	}
	if got.Releases[0].VersionCodes[0] != 42 {
		t.Fatalf("unexpected versions: %#v", got.Releases[0].VersionCodes)
	}
}

func TestStatusHumanOutput(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return []play.ReleaseSummary{{Name: "1.0", State: "PUBLISHED", VersionCodes: []int64{42}}}, nil
		},
	}

	out, errOut, code := runWithAPI(t, fake, "status", "-p", "com.example.app")
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.OK, errOut)
	}
	for _, want := range []string{"NAME", "1.0", "PUBLISHED", "42"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q misses %q", out, want)
		}
	}
}

func TestStatusEmpty(t *testing.T) {
	out, _, code := runWithAPI(t, &playtest.Fake{}, "status", "-p", "com.example.app")
	if code != int(exitcode.OK) {
		t.Fatalf("code = %d, want %d", code, exitcode.OK)
	}
	if !strings.Contains(out, "no releases on internal") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestStatusAPIError(t *testing.T) {
	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			return nil, exitcode.Errorf(exitcode.NotFound, "track not found")
		},
	}

	_, _, code := runWithAPI(t, fake, "status", "-p", "com.example.app")
	if code != int(exitcode.NotFound) {
		t.Fatalf("code = %d, want %d", code, exitcode.NotFound)
	}
}

func TestDoctorCancellationWritesOneJSONObject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fake := &playtest.Fake{
		TrackReleasesFunc: func(context.Context, string, string) ([]play.ReleaseSummary, error) {
			cancel()
			return nil, exitcode.Errorf(exitcode.Auth, "access denied")
		},
	}
	out, errOut, code := runWithAPIAndContext(ctx, t, fake, "--json", "doctor",
		"-p", "com.example.app", "-f", writeAAB(t))

	if code != int(exitcode.Interrupted) {
		t.Fatalf("code = %d, want %d (stderr: %q)", code, exitcode.Interrupted, errOut)
	}
	if !strings.Contains(errOut, "operation interrupted") {
		t.Fatalf("unexpected stderr: %q", errOut)
	}

	dec := json.NewDecoder(strings.NewReader(out))
	var first map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, out)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout must contain exactly one JSON object: %v (%q)", err, out)
	}
	if first["command"] != "doctor" {
		t.Fatalf("command = %v, want doctor", first["command"])
	}
	checks, _ := first["checks"].([]any)
	if len(checks) == 0 {
		t.Fatalf("the available checks must be retained: %#v", first)
	}
}
