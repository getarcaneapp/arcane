package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeRelativePath(t *testing.T) {
	for _, input := range []string{"", "/absolute", "../escape", "folder\\file", "name\x00"} {
		_, err := NormalizeRelativePath(input)
		require.Error(t, err, input)
	}
	got, err := NormalizeRelativePath("folder/file.txt")
	require.NoError(t, err)
	require.Equal(t, "folder/file.txt", got)
}

func TestValidateFileName(t *testing.T) {
	for _, input := range []string{"", ".", "..", "folder/name", "folder\\name", "name\x00"} {
		_, err := ValidateFileName(input)
		require.Error(t, err, input)
	}
	got, err := ValidateFileName("notes.txt")
	require.NoError(t, err)
	require.Equal(t, "notes.txt", got)
}

func TestFileTreeRevisionEntryIsStable(t *testing.T) {
	h := sha256.New()
	WriteFileTreeRevisionEntry(h, "folder/file.txt", "file", 12, 1234, "-rw-r--r--", false)
	require.Equal(t, "95f6a8dc033860a753fee2a04a21580d43087967d732a717d3e7a18f88ea4f65", hex.EncodeToString(h.Sum(nil)))
}

func TestSanitizeBrowsePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "current directory is root", input: ".", want: "/"},
		{name: "parent traversal is rejected", input: "..", wantErr: true},
		{name: "root stays root", input: "/", want: "/"},
		{name: "empty is root", input: "", want: "/"},
		{name: "relative path is rooted", input: "a/b", want: "/a/b"},
		{name: "absolute path is cleaned", input: "/a/../b", want: "/b"},
		{name: "escaping relative traversal is rejected", input: "a/../../../etc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeBrowsePath(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
