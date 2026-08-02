package selfupdate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindChecksumMatchesNextDistPath(t *testing.T) {
	checksums := "abc123  dist/arcane-cli_darwin_arm64_v8.0/arcane-cli\n"

	got, err := findChecksumInternal(checksums, "arcane-cli_darwin_arm64_v8.0/arcane-cli", "arcane-cli_darwin_arm64")

	require.NoError(t, err,
		"findChecksum returned error: %v", err)

	require.Equal(t, "abc123", got,
		"findChecksum = %q, want abc123", got)

}

func TestFindChecksumMatchesArchiveBasename(t *testing.T) {
	checksums := "def456  dist/arcane-cli_linux_amd64.tar.gz\n"

	got, err := findChecksumInternal(checksums, "arcane-cli_linux_amd64.tar.gz")

	require.NoError(t, err,
		"findChecksum returned error: %v", err)

	require.Equal(t, "def456", got,
		"findChecksum = %q, want def456", got)

}

func TestChecksumEntryNames(t *testing.T) {
	checksums := "abc123  ./arcane-cli_darwin_arm64\n\nbad-line\ndef456  dist/arcane-cli_linux_amd64.tar.gz\n"

	got := checksumEntryNamesInternal(checksums)
	want := []string{"arcane-cli_darwin_arm64", "dist/arcane-cli_linux_amd64.tar.gz"}
	if len(got) != len(want) {
		require.Len(t, got, len(want),
			"checksumEntryNames length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {

		require.Equal(t, want[i], got[i],
			"checksumEntryNames[%d] = %q, want %q", i, got[i], want[i])

	}
}

func TestCLIArtifactNamesUseFlatR2BinaryNames(t *testing.T) {
	platformName, err := cliPlatformNameInternal()

	require.NoError(t, err,
		"cliPlatformName returned error: %v", err)

	require.False(t, strings.HasPrefix(platformName, "arcane-cli_") || strings.Contains(platformName, "/") || strings.HasSuffix(platformName, ".tar.gz"),
		"platformName = %q, want bare platform name", platformName)

	artifactName, err := cliRawArtifactNameInternal()

	require.NoError(t, err,
		"cliRawArtifactName returned error: %v", err)

	require.Equal(t, "arcane-cli_"+platformName, artifactName,
		"artifactName = %q, want %q", artifactName, "arcane-cli_"+platformName)

}
