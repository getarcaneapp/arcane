package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectFilesystem_ResolveExisting_NormalizesOutsideRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := NewProjectFilesystem(ProjectFilesystemConfig{ProjectsRoot: root})

	workspace, err := fs.ResolveExisting(ProjectWorkspace{
		Name:    "Demo",
		DirName: "demo",
		Path:    filepath.Join(root, "..", "escape"),
	})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "demo"), workspace.Path)
}

func TestProjectFilesystem_StageCreateAndPromote_WritesFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := NewProjectFilesystem(ProjectFilesystemConfig{ProjectsRoot: root})

	workspace, err := fs.CreateWorkspace("Demo Project")
	require.NoError(t, err)

	envContent := "TOKEN=secret\n"
	staged, err := fs.StageCreate(workspace, "services:\n  app:\n    image: nginx:alpine\n", &envContent)
	require.NoError(t, err)
	require.NoError(t, fs.Promote(staged))

	composeBytes, err := os.ReadFile(filepath.Join(staged.Target.Path, "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:alpine")

	envBytes, err := os.ReadFile(filepath.Join(staged.Target.Path, ".env"))
	require.NoError(t, err)
	assert.Equal(t, envContent, string(envBytes))
}

func TestProjectFilesystem_StageCreateAndPromote_PreservesTraversableDirectoryPermissions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := NewProjectFilesystem(ProjectFilesystemConfig{ProjectsRoot: root})

	workspace, err := fs.CreateWorkspace("Demo Project")
	require.NoError(t, err)

	staged, err := fs.StageCreate(workspace, "services:\n  app:\n    image: nginx:alpine\n", nil)
	require.NoError(t, err)
	require.NoError(t, fs.Promote(staged))

	info, err := os.Stat(staged.Target.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestProjectFilesystem_Promote_RestoresOriginalOnFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectPath := filepath.Join(root, "demo")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:1\n"), 0o600))

	fs := NewProjectFilesystem(ProjectFilesystemConfig{ProjectsRoot: root})
	staged, err := fs.StageUpdate(ProjectWorkspace{
		Name:    "demo",
		DirName: "demo",
		Path:    projectPath,
	}, nil, ptrString("services:\n  app:\n    image: nginx:2\n"), nil)
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(staged.Stage.Path))

	err = fs.Promote(staged)
	require.Error(t, err)

	composeBytes, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, readErr)
	assert.Contains(t, string(composeBytes), "nginx:1")
}

func TestProjectFilesystem_Promote_CleansStageWhenRestoreFails(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "demo")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:1\n"), 0o600))

	fs := NewProjectFilesystem(ProjectFilesystemConfig{ProjectsRoot: root})
	staged, err := fs.StageUpdate(ProjectWorkspace{
		Name:    "demo",
		DirName: "demo",
		Path:    projectPath,
	}, nil, ptrString("services:\n  app:\n    image: nginx:2\n"), nil)
	require.NoError(t, err)

	var call int
	fs.renamePath = func(oldPath, newPath string) error {
		call++
		switch call {
		case 1:
			return nil
		case 2:
			return assert.AnError
		default:
			return assert.AnError
		}
	}

	err = fs.Promote(staged)
	require.Error(t, err)
	assert.ErrorContains(t, err, "restore failed")

	_, statErr := os.Stat(staged.Stage.Path)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestProjectFilesystem_LoadComposeMetadata_UsesComposeProjectName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectPath := filepath.Join(root, "demo")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(`name: ${COMPOSE_PROJECT_NAME}
services:
  app:
    image: nginx:alpine
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("COMPOSE_PROJECT_NAME=from-env\n"), 0o600))

	fs := NewProjectFilesystem(ProjectFilesystemConfig{ProjectsRoot: root})
	serviceCount, composeProjectName, err := fs.LoadComposeMetadata(context.Background(), ProjectWorkspace{
		Name:    "demo",
		DirName: "demo",
		Path:    projectPath,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, serviceCount)
	require.NotNil(t, composeProjectName)
	assert.Equal(t, "from-env", *composeProjectName)
}

func TestProjectFilesystem_StageGitSync_PersistsGitEnvState(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectPath := filepath.Join(root, "demo")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("TOKEN=local\nLOCAL_ONLY=1\n"), 0o600))

	fs := NewProjectFilesystem(ProjectFilesystemConfig{ProjectsRoot: root})
	gitEnv := "TOKEN=git\nREMOTE_ONLY=1\n"
	staged, err := fs.StageGitSync(ProjectWorkspace{
		Name:    "demo",
		DirName: "demo",
		Path:    projectPath,
	}, "services:\n  app:\n    image: nginx:2\n", &gitEnv)
	require.NoError(t, err)
	require.NoError(t, fs.Promote(staged))

	effectiveBytes, err := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, err)
	assert.Contains(t, string(effectiveBytes), "TOKEN=git\n")
	assert.Contains(t, string(effectiveBytes), "REMOTE_ONLY=1\n")
	assert.Contains(t, string(effectiveBytes), "LOCAL_ONLY=1\n")

	gitBytes, err := os.ReadFile(filepath.Join(projectPath, GitSourceEnvFileName))
	require.NoError(t, err)
	assert.Equal(t, gitEnv, string(gitBytes))

	overrideBytes, err := os.ReadFile(filepath.Join(projectPath, OverrideEnvFileName))
	require.NoError(t, err)
	assert.Equal(t, "LOCAL_ONLY=1\n", string(overrideBytes))
}

func ptrString(value string) *string {
	return &value
}
