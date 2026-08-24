package backup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarkSnapshotDirectoriesInternal(t *testing.T) {
	files := []string{"/volume", "/volume/folder", "/volume/file.txt", "/volume/link"}
	longOutput := "drwxr-xr-x root root 0 1 Jan 2026 00:00 \"/volume\"\r\n" +
		"drwxr-xr-x root root 0 1 Jan 2026 00:00 \"/volume/folder\"\r\n" +
		"-rw-r--r-- root root 5 1 Jan 2026 00:00 \"/volume/file.txt\"\r\n" +
		"lrwxrwxrwx root root 4 1 Jan 2026 00:00 \"/volume/link\" -> \"file.txt\""

	marked, err := markSnapshotDirectoriesInternal(files, longOutput)
	require.NoError(t, err)
	require.Equal(t, []string{"/volume/", "/volume/folder/", "/volume/file.txt", "/volume/link"}, marked)
	require.Equal(t, []string{"/volume", "/volume/folder", "/volume/file.txt", "/volume/link"}, files)
}

func TestMarkSnapshotDirectoriesRejectsMismatchedListingsInternal(t *testing.T) {
	_, err := markSnapshotDirectoriesInternal([]string{"/volume", "/volume/file.txt"}, "drwxr-xr-x root root 0 1 Jan 2026 00:00 \"/volume\"")
	require.ErrorContains(t, err, "different lengths")
}
