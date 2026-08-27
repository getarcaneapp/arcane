package volume

import (
	"testing"

	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/stretchr/testify/require"
)

func TestArchiveMembersForSelectionExpandsFoldersInternal(t *testing.T) {
	paths := []string{"./folder/", "./folder/a.txt", "./folder/nested/b.txt", "./other.txt"}
	selected := []backuptypes.BackupFileEntry{{Path: "folder", Name: "folder", IsDirectory: true}}
	require.Equal(
		t,
		[]string{"folder/", "folder/a.txt", "folder/nested/b.txt"},
		archiveMembersForSelectionInternal(paths, selected),
	)
}
