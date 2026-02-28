package version

import "runtime/debug"

// Build-time parameters set via -ldflags
var Version = "unknown"

// A user may install pug using `go install github.com/opencode-ai/opencode@latest`.
// without -ldflags, in which case the version above is unset. As a workaround
// we use the embedded build version that *is* set when using `go install` (and
// is only set for `go install` and not for `go build`).
func init() {
	// If Version was set via -ldflags, keep it
	if Version != "unknown" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	mainVersion := info.Main.Version
	if mainVersion == "" || mainVersion == "(devel)" {
		return
	}
	Version = mainVersion
}
