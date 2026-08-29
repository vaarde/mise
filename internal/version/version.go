// Package version records what build of Mise is running.
//
// The values are overridden at link time by the Makefile and by
// goreleaser. A build made with a plain `go build` reports itself as a
// development build rather than claiming to be a release.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Set via -ldflags at build time, e.g.
//
//	go build -ldflags "-X github.com/vaarde/mise/internal/version.Version=0.1.0"
var (
	// Version is the semantic version of this build.
	Version = "0.1.0-dev"

	// Commit is the git revision this was built from.
	Commit = ""

	// BuildDate is when the binary was built, in RFC3339.
	BuildDate = ""
)

// Info describes a build.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Current returns the running build's details.
//
// When ldflags did not supply a commit, it falls back to the revision Go
// stamps into the binary from version control, so even a plain
// `go build` can usually say what it was built from.
func Current() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	if info.Commit == "" {
		info.Commit = vcsRevision()
	}
	return info
}

// vcsRevision reads the git revision Go embeds in the build info.
func vcsRevision() string {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	var revision, modified string
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}

	if revision == "" {
		return ""
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified == "true" {
		// Mark a binary built from a dirty tree, so a bug report from it
		// is not mistaken for one against the clean commit.
		revision += "-dirty"
	}
	return revision
}

// String renders the one-line form: "mise 0.1.0-dev (abc123def456)".
func (i Info) String() string {
	out := "mise " + i.Version
	if i.Commit != "" {
		out += " (" + i.Commit + ")"
	}
	return out
}

// Detailed renders the multi-line form shown by `mise version`.
func (i Info) Detailed() string {
	var b strings.Builder

	fmt.Fprintf(&b, "mise %s\n", i.Version)
	if i.Commit != "" {
		fmt.Fprintf(&b, "  commit:   %s\n", i.Commit)
	}
	if i.BuildDate != "" {
		fmt.Fprintf(&b, "  built:    %s\n", i.BuildDate)
	}
	fmt.Fprintf(&b, "  go:       %s\n", i.GoVersion)
	fmt.Fprintf(&b, "  platform: %s\n", i.Platform)

	return b.String()
}
