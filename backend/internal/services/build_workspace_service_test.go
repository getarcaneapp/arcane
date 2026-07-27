package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildWorkspaceServiceDeleteFileRejectsCurrentDirectoryInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	root := t.TempDir()
	require.NoError(t, settingsService.UpdateSetting(ctx, "buildsDirectory", root))
	sentinel := filepath.Join(root, "keep.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0o600))

	err = NewBuildWorkspaceService(settingsService).DeleteFile(ctx, ".")
	require.ErrorContains(t, err, "cannot delete root directory")
	require.FileExists(t, sentinel)
}
