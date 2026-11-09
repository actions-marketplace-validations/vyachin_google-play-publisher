package artifact_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/artifact"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

func writeJSON(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "output-metadata.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	return path
}

func TestReadMetadata(t *testing.T) {
	path := writeJSON(t, `{
		"version": 3,
		"artifactType": {"type": "BUNDLE", "kind": "Directory"},
		"applicationId": "com.example.app",
		"variantName": "release",
		"elements": [
			{"type": "SINGLE", "versionCode": 42, "versionName": "1.2.3", "outputFile": "app-release.aab"}
		]
	}`)

	meta, err := artifact.ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	if meta.ApplicationID != "com.example.app" {
		t.Errorf("ApplicationID = %q", meta.ApplicationID)
	}
	if meta.VersionCode != 42 {
		t.Errorf("VersionCode = %d, want 42", meta.VersionCode)
	}
	if meta.VersionName != "1.2.3" {
		t.Errorf("VersionName = %q", meta.VersionName)
	}
}

func TestReadMetadataErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "invalid json",
			body: `{`,
			want: "parse",
		},
		{
			name: "wrong artifact type",
			body: `{"artifactType":{"type":"APK"},"applicationId":"com.example.app","elements":[{"versionCode":1}]}`,
			want: "want BUNDLE",
		},
		{
			name: "no application id",
			body: `{"elements":[{"versionCode":1}]}`,
			want: "applicationId",
		},
		{
			name: "no elements",
			body: `{"applicationId":"com.example.app","elements":[]}`,
			want: "elements",
		},
		{
			name: "multiple elements",
			body: `{"applicationId":"com.example.app","elements":[{"versionCode":1},{"versionCode":2}]}`,
			want: "elements",
		},
		{
			name: "invalid version code",
			body: `{"applicationId":"com.example.app","elements":[{"versionCode":0}]}`,
			want: "versionCode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeJSON(t, tt.body)
			_, err := artifact.ReadMetadata(path)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := exitcode.From(err); got != exitcode.Usage {
				t.Fatalf("code = %d, want %d", got, exitcode.Usage)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestVerifyPackage(t *testing.T) {
	meta := artifact.Metadata{ApplicationID: "com.example.app"}

	if err := meta.VerifyPackage("com.example.app"); err != nil {
		t.Fatalf("VerifyPackage: %v", err)
	}

	err := meta.VerifyPackage("com.other.app")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Usage {
		t.Fatalf("code = %d, want %d", got, exitcode.Usage)
	}
}
