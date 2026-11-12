package main

import (
	"runtime/debug"
	"strings"
)

// version can be set at build time with -ldflags "-X main.version=1.2.3".
// Otherwise it is taken from the build information that the Go toolchain
// records: the module version for `go install ...@vX.Y.Z`, the git tag (or a
// pseudo-version for untagged commits) for `go build` in a git checkout.
var version = ""

func currentVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return versionFromBuildInfo(info.Main.Version)
	}
	return "dev"
}

func versionFromBuildInfo(v string) string {
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return strings.TrimPrefix(v, "v")
}
