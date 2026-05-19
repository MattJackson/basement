// Package version exposes build-time metadata stamped via -ldflags.
package version

// Build-time metadata. Defaults assume a local dev build; CI overrides
// via -ldflags="-X 'github.com/mattjackson/basement/internal/version.Version=vX.Y.Z' ...".
var (
	// Version is the semver tag (e.g. "v0.1.2") or "dev" for local builds.
	Version = "dev"
	// Commit is the short git SHA the binary was built from.
	Commit = "unknown"
	// BuiltAt is the build timestamp in RFC 3339 (UTC).
	BuiltAt = "unknown"
)

// Info bundles the version metadata for JSON encoding.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
}

// Get returns the current build's version info.
func Get() Info {
	return Info{Version: Version, Commit: Commit, BuiltAt: BuiltAt}
}
