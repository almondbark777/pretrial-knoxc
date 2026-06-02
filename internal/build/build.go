// Package build carries build-time identification, injected at link time so the
// running process can report exactly which build it is — handy for confirming a
// deploy actually took ("is the new binary live?"). Surfaced in /health and as
// the ptr_build_info metric.
package build

// Version is the build identifier (typically "<git-short-sha>-<date>"), set via:
//
//	go build -ldflags "-X pretrial-knoxc/internal/build.Version=abc1234-20260531"
//
// It defaults to "dev" for un-stamped local builds.
var Version = "dev"
