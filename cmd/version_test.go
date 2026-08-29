package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/version"
)

func runVersionCmd(t *testing.T, asJSON bool) string {
	t.Helper()

	prev := versionJSON
	t.Cleanup(func() { versionJSON = prev })
	versionJSON = asJSON

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	require.NoError(t, runVersion(cmd, nil))
	return out.String()
}

func TestVersionPrintsBuildDetails(t *testing.T) {
	out := runVersionCmd(t, false)

	assert.Contains(t, out, "mise ")
	assert.Contains(t, out, version.Version)
	assert.Contains(t, out, "go:")
	assert.Contains(t, out, "platform:",
		"a bug report needs the platform, not just the version")
}

func TestVersionJSON(t *testing.T) {
	out := runVersionCmd(t, true)

	var info version.Info
	require.NoError(t, json.Unmarshal([]byte(out), &info))

	assert.Equal(t, version.Version, info.Version)
	assert.NotEmpty(t, info.GoVersion)
	assert.NotEmpty(t, info.Platform)
}

func TestVersionInfoString(t *testing.T) {
	assert.Equal(t, "mise 1.2.3", version.Info{Version: "1.2.3"}.String())
	assert.Equal(t, "mise 1.2.3 (abc123)", version.Info{Version: "1.2.3", Commit: "abc123"}.String())
}

func TestVersionDetailedOmitsUnknownFields(t *testing.T) {
	// A plain `go build` has no injected commit or build date; those
	// lines should be absent rather than printed as empty.
	detailed := version.Info{Version: "1.2.3", GoVersion: "go1.22", Platform: "linux/amd64"}.Detailed()

	assert.Contains(t, detailed, "mise 1.2.3")
	assert.NotContains(t, detailed, "commit:")
	assert.NotContains(t, detailed, "built:")
	assert.Contains(t, detailed, "go:")
}

func TestVersionCurrentFillsInRuntimeDetails(t *testing.T) {
	info := version.Current()

	assert.NotEmpty(t, info.Version)
	assert.NotEmpty(t, info.GoVersion)
	assert.Contains(t, info.Platform, "/")
}
