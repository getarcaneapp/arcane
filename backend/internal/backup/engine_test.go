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

func TestQualifySnapshotListingInternal(t *testing.T) {
	tests := []struct {
		name         string
		files        []string
		snapshotPath string
		expected     []string
	}{
		{
			name:         "root listing",
			files:        []string{"folder/", "file.txt"},
			snapshotPath: "",
			expected:     []string{"folder/", "file.txt"},
		},
		{
			name:         "empty listing",
			files:        []string{},
			snapshotPath: "folder",
			expected:     []string{},
		},
		{
			name:         "nested listing",
			files:        []string{"nested/", "file.txt", "./link"},
			snapshotPath: "folder/",
			expected:     []string{"folder/nested/", "folder/file.txt", "folder/link"},
		},
		{
			name:         "legacy system project listing",
			files:        []string{"demo/compose.yaml"},
			snapshotPath: "/app/data/projects/",
			expected:     []string{"app/data/projects/demo/compose.yaml"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qualified := qualifySnapshotListingInternal(test.files, test.snapshotPath)
			require.Equal(t, test.expected, qualified)
			if len(test.files) > 0 {
				require.NotSame(t, &test.files[0], &qualified[0])
			}
		})
	}
}
