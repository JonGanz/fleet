package main

import "runtime/debug"

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

// versionString returns version, or for an unstamped "dev" build the VCS
// revision Go embedded at build time (when built inside a git checkout).
func versionString() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	return versionFromSettings(version, info.Settings)
}

// versionFromSettings is versionString's pure part: "dev-<short revision>"
// plus "-dirty" for a modified tree, or fallback without a revision.
func versionFromSettings(fallback string, settings []debug.BuildSetting) string {
	var revision string
	modified := false
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return fallback
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	v := fallback + "-" + revision
	if modified {
		v += "-dirty"
	}
	return v
}
