package restore

import (
	"os/exec"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/stretchr/testify/require"
)

func TestArchiveMembersForSelectionExpandsFolders(t *testing.T) {
	paths := []string{"./folder/", "./folder/a.txt", "./folder/nested/b.txt", "./other.txt"}
	selected := []backup.BackupFileEntry{{Path: "folder", Name: "folder", IsDirectory: true}}
	require.Equal(
		t,
		[]string{"folder/", "folder/a.txt", "folder/nested/b.txt"},
		archiveMembersForSelection(paths, selected),
	)
}

func TestRestoreBackupFilesScriptUsesSupportedTooling(t *testing.T) {
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
	} {
		require.NotContains(t, restoreBackupFilesScript, unsupported)
	}
	if shellPath, err := exec.LookPath("sh"); err == nil {
		output, syntaxErr := exec.Command(shellPath, "-n", "-c", restoreBackupFilesScript).CombinedOutput()
		require.NoErrorf(t, syntaxErr, "restore script has invalid syntax: %s", output)
	}
}
