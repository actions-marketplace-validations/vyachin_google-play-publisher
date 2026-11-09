package artifact

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

// Metadata is the subset of AGP output-metadata.json used by gplay.
type Metadata struct {
	ApplicationID string
	VersionCode   int64
	VersionName   string
}

type rawMetadata struct {
	ArtifactType struct {
		Type string `json:"type"`
	} `json:"artifactType"`
	ApplicationID string `json:"applicationId"`
	Elements      []struct {
		VersionCode int64  `json:"versionCode"`
		VersionName string `json:"versionName"`
	} `json:"elements"`
}

// ReadMetadata parses output-metadata.json produced by the Android Gradle Plugin.
func ReadMetadata(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, exitcode.WithHint(
			exitcode.Wrap(exitcode.Usage, err),
			"check the path passed to --metadata",
		)
	}

	var raw rawMetadata
	if err := json.Unmarshal(data, &raw); err != nil {
		return Metadata{}, exitcode.Wrap(exitcode.Usage, fmt.Errorf("parse %s: %w", path, err))
	}

	if raw.ArtifactType.Type != "" && raw.ArtifactType.Type != "BUNDLE" {
		return Metadata{}, exitcode.Errorf(exitcode.Usage,
			"%s describes artifact type %q, want BUNDLE", path, raw.ArtifactType.Type)
	}
	if raw.ApplicationID == "" {
		return Metadata{}, exitcode.Errorf(exitcode.Usage, "%s has no applicationId", path)
	}
	if len(raw.Elements) != 1 {
		return Metadata{}, exitcode.Errorf(exitcode.Usage,
			"%s has %d elements, want exactly 1", path, len(raw.Elements))
	}

	element := raw.Elements[0]
	if element.VersionCode <= 0 {
		return Metadata{}, exitcode.Errorf(exitcode.Usage,
			"%s has invalid versionCode %d", path, element.VersionCode)
	}

	return Metadata{
		ApplicationID: raw.ApplicationID,
		VersionCode:   element.VersionCode,
		VersionName:   element.VersionName,
	}, nil
}

// VerifyPackage checks that metadata belongs to the given application id.
func (m Metadata) VerifyPackage(pkg string) error {
	if m.ApplicationID != pkg {
		return exitcode.Errorf(exitcode.Usage,
			"metadata applicationId %q does not match --package %q", m.ApplicationID, pkg)
	}
	return nil
}
