package app

import (
	"github.com/vyachin/google-play-publisher/internal/artifact"
	"github.com/vyachin/google-play-publisher/internal/exitcode"
)

// localIdentity is the release identity resolved from the AAB base manifest.
type localIdentity struct {
	Package     string
	VersionCode int64
	VersionName string
}

// validateIdentity compares the AAB manifest identity with a package override,
// an optional version-code override and optional AGP metadata. Every conflict
// is a local input error (exit code 2) and is detected before networking.
func validateIdentity(
	identity artifact.Identity,
	packageID string,
	versionCode int64,
	meta *artifact.Metadata,
) (localIdentity, error) {
	resolved := localIdentity{
		Package:     identity.ApplicationID,
		VersionCode: identity.VersionCode,
		VersionName: identity.VersionName,
	}

	if packageID != "" && packageID != identity.ApplicationID {
		return localIdentity{}, exitcode.Errorf(exitcode.Usage,
			"--package %q does not match AAB applicationId %q", packageID, identity.ApplicationID)
	}
	if meta != nil {
		if meta.ApplicationID != identity.ApplicationID {
			return localIdentity{}, exitcode.Errorf(exitcode.Usage,
				"metadata applicationId %q does not match AAB applicationId %q",
				meta.ApplicationID, identity.ApplicationID)
		}
		if meta.VersionCode != identity.VersionCode {
			return localIdentity{}, exitcode.Errorf(exitcode.Usage,
				"metadata versionCode %d does not match AAB versionCode %d",
				meta.VersionCode, identity.VersionCode)
		}
	}
	if versionCode > 0 && versionCode != identity.VersionCode {
		return localIdentity{}, exitcode.Errorf(exitcode.Usage,
			"--version-code %d does not match AAB versionCode %d",
			versionCode, identity.VersionCode)
	}

	return resolved, nil
}
