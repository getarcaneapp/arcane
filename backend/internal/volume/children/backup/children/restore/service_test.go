package restore

import (
	"os/exec"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"
)

func TestArchiveMembersForSelectionExpandsFolders(t *testing.T) {
	paths := []string{"./folder/", "./folder/a.txt", "./folder/nested/b.txt", "./other.txt"}
	selected := []types.BackupFileEntry{{Path: "folder", Name: "folder", IsDirectory: true}}
	require.Equal(
		t,
		[]string{"folder/", "folder/a.txt", "folder/nested/b.txt"},
		archiveMembersForSelection(paths, selected),
	)
}

func TestRestoreBackupFilesScriptUsesSupportedTooling(t *testing.T) {
	for _, script := range []string{restoreBackupFilesScript, replaceVolumeScript, restoreArchiveScript} {
		for _, unsupported := range []string{
			"if [",
			"elif [",
			"while [",
			"test ",
			"local ",
			"find -P",
			"-printf",
			"sort -z",
			"xargs",
			"cat --",
			"dirname",
			"install ",
			"--files-from",
			"tar -r",
			"$((",
			"-not",
			"-exec",
			"-quit",
		} {
			require.NotContains(t, script, unsupported)
		}
		if shellPath, err := exec.LookPath("sh"); err == nil {
			output, syntaxErr := exec.Command(shellPath, "-n", "-c", script).CombinedOutput()
			require.NoErrorf(t, syntaxErr, "restore script has invalid syntax: %s", output)
		}
		require.NotContains(t, script, "rm -rf /volume/*")
		require.NotContains(t, script, "2>/dev/null || true")
	}
	require.Contains(t, replaceVolumeScript, `case "$f" in "$1")`)
}
