// Package version holds the build's version string, set with
// -ldflags "-X elsewhere/internal/version.Build=v1.2.3".
package version

// Build is "dev" unless a release build overrides it.
var Build = "dev"
