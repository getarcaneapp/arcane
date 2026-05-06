package selfupdate

import "testing"

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
