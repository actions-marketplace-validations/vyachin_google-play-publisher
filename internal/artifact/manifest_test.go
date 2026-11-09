package artifact

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/artifact/artifacttest"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestParseManifestIdentity(t *testing.T) {
	identity, err := parseManifest(artifacttest.Manifest("com.example.app", 42, "1.0.42"))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if identity.ApplicationID != "com.example.app" || identity.VersionCode != 42 || identity.VersionName != "1.0.42" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
}

func TestParseManifestWithoutVersionName(t *testing.T) {
	identity, err := parseManifest(artifacttest.Manifest("com.example.app", 42, ""))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if identity.VersionName != "" {
		t.Fatalf("version name = %q, want empty", identity.VersionName)
	}
}

func TestParseManifestRejectsInvalidIdentity(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		errSub string
	}{
		{
			name:   "missing package",
			data:   buildManifest(manifestAttribute(androidNamespace, "versionCode", "42", itemPrim(42))),
			errSub: "package",
		},
		{
			name:   "missing version code",
			data:   buildManifest(manifestAttribute("", "package", "com.example.app", nil)),
			errSub: "versionCode",
		},
		{
			name: "zero version code",
			data: buildManifest(
				manifestAttribute("", "package", "com.example.app", nil),
				manifestAttribute(androidNamespace, "versionCode", "0", itemPrim(0)),
			),
			errSub: "not positive",
		},
		{
			name: "versionCodeMajor",
			data: buildManifest(
				manifestAttribute("", "package", "com.example.app", nil),
				manifestAttribute(androidNamespace, "versionCode", "42", itemPrim(42)),
				manifestAttribute(androidNamespace, "versionCodeMajor", "1", itemPrim(1)),
			),
			errSub: "versionCodeMajor",
		},
		{
			name: "resource reference",
			data: buildManifest(
				manifestAttribute("", "package", "com.example.app", nil),
				manifestAttribute(androidNamespace, "versionCode", "@integer/version", itemReference("version")),
			),
			errSub: "resource reference",
		},
		{
			name: "duplicate package",
			data: buildManifest(
				manifestAttribute("", "package", "com.example.app", nil),
				manifestAttribute("", "package", "com.example.other", nil),
			),
			errSub: "duplicate",
		},
		{
			name: "wrong root",
			data: buildManifestWithName("application",
				manifestAttribute("", "package", "com.example.app", nil),
				manifestAttribute(androidNamespace, "versionCode", "42", itemPrim(42)),
			),
			errSub: "manifest",
		},
		{
			name:   "malformed protobuf",
			data:   []byte{0xff, 0x00},
			errSub: "wire type",
		},
		{
			name:   "truncated",
			data:   artifacttest.Manifest("com.example.app", 42, "1.0.42")[:12],
			errSub: "EOF",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseManifest(test.data)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), test.errSub) {
				t.Fatalf("error %q does not contain %q", err, test.errSub)
			}
		})
	}
}

func TestOpenReadsIdentityFromRealManifest(t *testing.T) {
	bundle, err := Open(filepath.Join("testdata", "identity.aab"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if bundle.Identity.ApplicationID != "com.example.fixture" {
		t.Errorf("application id = %q", bundle.Identity.ApplicationID)
	}
	if bundle.Identity.VersionCode != 42 {
		t.Errorf("version code = %d", bundle.Identity.VersionCode)
	}
	if bundle.Identity.VersionName != "1.0.42" {
		t.Errorf("version name = %q", bundle.Identity.VersionName)
	}
	if bundle.Info.Size == 0 || bundle.Info.SHA256 == "" {
		t.Errorf("inspection result is incomplete: %#v", bundle.Info)
	}
	if err := bundle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := bundle.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if err := bundle.Rewind(); err == nil {
		t.Fatal("rewind after close must fail")
	}
}

func TestOpenRejectsMissingManifest(t *testing.T) {
	path := writeBundle(t, map[string][]byte{"BundleConfig.pb": []byte("config")})
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "base/manifest/AndroidManifest.xml") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenRejectsOversizedManifest(t *testing.T) {
	path := writeBundle(t, map[string][]byte{
		"BundleConfig.pb":                   []byte("config"),
		"base/manifest/AndroidManifest.xml": make([]byte, maxManifestSize+1),
	})
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInspectDoesNotRequireManifest(t *testing.T) {
	path := writeBundle(t, map[string][]byte{"BundleConfig.pb": []byte("config")})
	if _, err := Inspect(path); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
}

func writeBundle(t *testing.T, files map[string][]byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app-release.aab")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	writer := zip.NewWriter(file)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := entry.Write(body); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return path
}

func buildManifest(attributes ...[]byte) []byte {
	return buildManifestWithName("manifest", attributes...)
}

func buildManifestWithName(name string, attributes ...[]byte) []byte {
	var element []byte
	element = appendTestString(element, 3, name)
	for _, attribute := range attributes {
		element = appendTestBytes(element, 4, attribute)
	}
	var node []byte
	node = appendTestBytes(node, 1, element)
	return node
}

func manifestAttribute(namespaceURI, name, value string, item []byte) []byte {
	var attribute []byte
	if namespaceURI != "" {
		attribute = appendTestString(attribute, 1, namespaceURI)
	}
	attribute = appendTestString(attribute, 2, name)
	attribute = appendTestString(attribute, 3, value)
	if item != nil {
		attribute = appendTestBytes(attribute, 6, item)
	}
	return attribute
}

func itemPrim(value int64) []byte {
	var primitive []byte
	primitive = appendTestVarint(primitive, 6, uint64(value))
	var item []byte
	item = appendTestBytes(item, 7, primitive)
	return item
}

func itemReference(name string) []byte {
	var reference []byte
	reference = appendTestString(reference, 3, name)
	var item []byte
	item = appendTestBytes(item, 1, reference)
	return item
}

func appendTestString(dst []byte, number protowire.Number, value string) []byte {
	dst = protowire.AppendTag(dst, number, protowire.BytesType)
	return protowire.AppendString(dst, value)
}

func appendTestBytes(dst []byte, number protowire.Number, value []byte) []byte {
	dst = protowire.AppendTag(dst, number, protowire.BytesType)
	return protowire.AppendBytes(dst, value)
}

func appendTestVarint(dst []byte, number protowire.Number, value uint64) []byte {
	dst = protowire.AppendTag(dst, number, protowire.VarintType)
	return protowire.AppendVarint(dst, value)
}
