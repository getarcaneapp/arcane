package selfupdate

import (
	"strings"
	"testing"
)

func TestFindChecksumMatchesNextDistPath(t *testing.T) {
	checksums := "abc123  dist/arcane-cli_darwin_arm64_v8.0/arcane-cli\n"

	got, err := findChecksumInternal(checksums, "arcane-cli_darwin_arm64_v8.0/arcane-cli", "arcane-cli_darwin_arm64")
	if err != nil {
		t.Fatalf("findChecksum returned error: %v", err)
	}
	if got != "abc123" {
		t.Fatalf("findChecksum = %q, want abc123", got)
	}
}

func TestFindChecksumMatchesArchiveBasename(t *testing.T) {
	checksums := "def456  dist/arcane-cli_linux_amd64.tar.gz\n"

	got, err := findChecksumInternal(checksums, "arcane-cli_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("findChecksum returned error: %v", err)
	}
	if got != "def456" {
		t.Fatalf("findChecksum = %q, want def456", got)
	}
}

func TestChecksumEntryNames(t *testing.T) {
	checksums := "abc123  ./arcane-cli_darwin_arm64\n\nbad-line\ndef456  dist/arcane-cli_linux_amd64.tar.gz\n"

	got := checksumEntryNamesInternal(checksums)
	want := []string{"arcane-cli_darwin_arm64", "dist/arcane-cli_linux_amd64.tar.gz"}
	if len(got) != len(want) {
		t.Fatalf("checksumEntryNames length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("checksumEntryNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCLIArtifactNamesUseFlatR2BinaryNames(t *testing.T) {
	platformName, err := cliPlatformNameInternal()
	if err != nil {
		t.Fatalf("cliPlatformName returned error: %v", err)
	}
	if strings.HasPrefix(platformName, "arcane-cli_") || strings.Contains(platformName, "/") || strings.HasSuffix(platformName, ".tar.gz") {
		t.Fatalf("platformName = %q, want bare platform name", platformName)
	}

	artifactName, err := cliRawArtifactNameInternal()
	if err != nil {
		t.Fatalf("cliRawArtifactName returned error: %v", err)
	}
	if artifactName != "arcane-cli_"+platformName {
		t.Fatalf("artifactName = %q, want %q", artifactName, "arcane-cli_"+platformName)
	}
}
