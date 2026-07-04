// Package version exposes the build version, stamped at build time via ldflags.
package version

// Version is overridden at build time with:
//
//	-ldflags "-X github.com/giovi321/server-status/internal/version.Version=v1.2.3"
var Version = "dev"
