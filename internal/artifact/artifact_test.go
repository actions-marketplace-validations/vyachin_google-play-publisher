package artifact_test

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/artifact"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

func writeBundle(t *testing.T, files map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app-release.aab")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return path
}

func TestInspect(t *testing.T) {
	path := writeBundle(t, map[string]string{
		"BundleConfig.pb":                   "bundle-config",
		"base/manifest/AndroidManifest.xml": "manifest",
	})

	info, err := artifact.Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	sum := sha256.Sum256(data)

	if info.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("SHA256 = %q, want %q", info.SHA256, hex.EncodeToString(sum[:]))
	}
	if info.Size != int64(len(data)) {
		t.Errorf("Size = %d, want %d", info.Size, len(data))
	}
	if info.Path != path {
		t.Errorf("Path = %q, want %q", info.Path, path)
	}
	if info.Signed {
		t.Error("unsigned bundle reported as signed")
	}
}

func TestInspectDetectsSignature(t *testing.T) {
	path := writeBundle(t, map[string]string{
		"BundleConfig.pb":   "bundle-config",
		"META-INF/CERT.RSA": "signature",
	})

	info, err := artifact.Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !info.Signed {
		t.Fatal("signed bundle reported as unsigned")
	}
}

func TestInspectRejectsNonBundle(t *testing.T) {
	tests := []struct {
		name string
		file map[string]string
	}{
		{name: "zip without BundleConfig", file: map[string]string{"AndroidManifest.xml": "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeBundle(t, tt.file)
			_, err := artifact.Inspect(path)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := exitcode.From(err); got != exitcode.Usage {
				t.Fatalf("code = %d, want %d", got, exitcode.Usage)
			}
			if !strings.Contains(err.Error(), "BundleConfig.pb") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestInspectRejectsPlainFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.apk")
	if err := os.WriteFile(path, []byte("not a zip"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := artifact.Inspect(path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Usage {
		t.Fatalf("code = %d, want %d", got, exitcode.Usage)
	}
}

func TestInspectMissingFile(t *testing.T) {
	_, err := artifact.Inspect(filepath.Join(t.TempDir(), "missing.aab"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := exitcode.From(err); got != exitcode.Usage {
		t.Fatalf("code = %d, want %d", got, exitcode.Usage)
	}
}
