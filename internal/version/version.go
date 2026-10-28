// Package version reports the Boombrick build version.
package version

// Version is overridden at build time:
//
//	-ldflags "-X github.com/ujjwal313/boombrick/internal/version.Version=1.0.0"
var Version = "dev"

// String returns the name and version shown in Settings and the log.
func String() string {
	return "AnberTunes " + Version
}
