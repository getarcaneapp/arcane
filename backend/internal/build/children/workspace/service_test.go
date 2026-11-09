package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/types/v3"
)

func TestServiceReadListsBuildsDirectory(t *testing.T) {
	service, root := newTestService(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "Dockerfile"), []byte("FROM scratch\n"), 0o600))

	result, err := service.Read(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, result.FileTreeRevision)
	require.False(t, result.FileTreeTruncated)

	paths := make([]string, 0, len(result.Files))
	for _, file := range result.Files {
		paths = append(paths, file.RelativePath)
	}
	require.Equal(t, []string{"app", "app/Dockerfile"}, paths)
}

func TestServiceFileReadsTextContent(t *testing.T) {
	service, root := newTestService(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "blob.bin"), []byte{0xff, 0x00}, 0o600))

	file, err := service.File(t.Context(), "Dockerfile")
	require.NoError(t, err)
	require.Equal(t, "FROM scratch\n", file.Content)
	require.True(t, file.Editable)

	binary, err := service.File(t.Context(), "blob.bin")
	require.NoError(t, err)
	require.False(t, binary.Editable)
	require.Equal(t, types.FileReadOnlyBinary, binary.ReadOnlyReason)

	_, err = service.File(t.Context(), "missing.txt")
	require.ErrorIs(t, err, common.ErrNotFound)
}

func TestServiceUpdateAppliesChanges(t *testing.T) {
	service, root := newTestService(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.txt"), []byte("old\n"), 0o600))

	current, err := service.Read(t.Context())
	require.NoError(t, err)

	result, err := service.Update(t.Context(), types.WorkspaceUpdateManifest{
		FileTreeRevision: current.FileTreeRevision,
		FileChanges: []types.WorkspaceFileChange{
			{Operation: types.FileOpCreateFolder, RelativePath: "app"},
			{Operation: types.FileOpCreateFile, RelativePath: "app/Dockerfile", UploadIndex: new(0)},
			{Operation: types.FileOpRename, RelativePath: "app/Dockerfile", NewName: "Containerfile"},
			{Operation: types.FileOpDelete, RelativePath: "old.txt"},
		},
	}, map[int][]byte{0: []byte("FROM scratch\n")})
	require.NoError(t, err)
	require.NotEqual(t, current.FileTreeRevision, result.FileTreeRevision)

	content, err := os.ReadFile(filepath.Join(root, "app", "Containerfile"))
	require.NoError(t, err)
	require.Equal(t, "FROM scratch\n", string(content))
	require.NoFileExists(t, filepath.Join(root, "app", "Dockerfile"))
	require.NoFileExists(t, filepath.Join(root, "old.txt"))

	_, err = service.Update(t.Context(), types.WorkspaceUpdateManifest{
		FileTreeRevision: current.FileTreeRevision,
		FileChanges:      []types.WorkspaceFileChange{{Operation: types.FileOpCreateFolder, RelativePath: "stale"}},
	}, nil)
	require.ErrorIs(t, err, common.ErrConflict)
	require.NoDirExists(t, filepath.Join(root, "stale"))
}

func TestServiceRejectsPathEscape(t *testing.T) {
	service, root := newTestService(t)
	current, err := service.Read(t.Context())
	require.NoError(t, err)

	_, err = service.Update(t.Context(), types.WorkspaceUpdateManifest{
		FileTreeRevision: current.FileTreeRevision,
		FileChanges:      []types.WorkspaceFileChange{{Operation: types.FileOpCreateFile, RelativePath: "../escape.txt", UploadIndex: new(0)}},
	}, map[int][]byte{0: []byte("escape\n")})
	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(filepath.Dir(root), "escape.txt"))

	_, err = service.File(t.Context(), "../escape.txt")
	require.ErrorIs(t, err, common.ErrForbidden)

	_, _, _, err = service.Download(t.Context(), "../../etc/passwd")
	require.ErrorIs(t, err, common.ErrForbidden)
}

func TestServiceUploadWritesFileIntoParent(t *testing.T) {
	service, root := newTestService(t)
	current, err := service.Read(t.Context())
	require.NoError(t, err)

	result, err := service.Upload(t.Context(), "app/assets", "large.bin", strings.NewReader("first"), 5)
	require.NoError(t, err)
	require.NotEqual(t, current.FileTreeRevision, result.FileTreeRevision)
	content, err := os.ReadFile(filepath.Join(root, "app", "assets", "large.bin"))
	require.NoError(t, err)
	require.Equal(t, "first", string(content))

	_, err = service.Upload(t.Context(), "app/assets", "large.bin", strings.NewReader("second"), 6)
	require.NoError(t, err)
	content, err = os.ReadFile(filepath.Join(root, "app", "assets", "large.bin"))
	require.NoError(t, err)
	require.Equal(t, "second", string(content))
}

func TestServiceUploadRejectsPathEscape(t *testing.T) {
	service, root := newTestService(t)

	_, err := service.Upload(t.Context(), "../outside", "escape.txt", strings.NewReader("escape"), 6)
	require.ErrorIs(t, err, common.ErrForbidden)
	require.NoFileExists(t, filepath.Join(filepath.Dir(root), "outside", "escape.txt"))

	_, err = service.Upload(t.Context(), "", "../escape.txt", strings.NewReader("escape"), 6)
	require.ErrorIs(t, err, common.ErrBadRequest)
	require.NoFileExists(t, filepath.Join(filepath.Dir(root), "escape.txt"))
}

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     &database.DB{DB: db},
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })

	root := filepath.Join(t.TempDir(), "builds")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, settingsService.UpdateSetting(t.Context(), "buildsDirectory", root))
	return NewService(settingsService), root
}
