// Package version exposes build metadata for the gplay binary.
package version

import (
	"fmt"
	"runtime"
)

// Build metadata. Values are overridden at build time with -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Info describes the build a binary was produced from.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
}

// New returns build information including the Go version used to build the binary.
func New() Info {
	return Info{Version: Version, Commit: Commit, Date: Date, Go: runtime.Version()}
}

// String renders the build information for humans.
func (i Info) String() string {
	return fmt.Sprintf("%s (commit %s, built %s, %s)", i.Version, i.Commit, i.Date, i.Go)
}
