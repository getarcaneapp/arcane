package services

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/stretchr/testify/require"
)

func TestVolumeWorkspaceScriptsAgainstToolsImage(t *testing.T) {
	if os.Getenv("ARCANE_VOLUME_WORKSPACE_DOCKER_TEST") != "1" {
		t.Skip("set ARCANE_VOLUME_WORKSPACE_DOCKER_TEST=1 to run the helper image integration test")
	}
	dockerPath, err := exec.LookPath("docker")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	runDocker := func(args ...string) string {
		t.Helper()
		output, err := exec.CommandContext(ctx, dockerPath, args...).CombinedOutput()
		require.NoErrorf(t, err, "docker %s\n%s", strings.Join(args, " "), output)
		return string(output)
	}
	runInVolume := func(volumeName, outerScript string, args ...string) string {
		t.Helper()
		dockerArgs := []string{"run", "--rm", "-v", volumeName + ":/volume", volumehelper.DefaultToolsImage, "sh", "-c", outerScript, "sh"}
		dockerArgs = append(dockerArgs, args...)
		return runDocker(dockerArgs...)
	}

	volumeName := "arcane-workspace-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	runDocker("volume", "create", volumeName)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = exec.CommandContext(cleanupCtx, dockerPath, "volume", "rm", volumeName).CombinedOutput()
	})

	runInVolume(volumeName, `mkdir -p /volume/folder/nested
printf child > /volume/folder/nested/child.txt
printf hidden > /volume/.hidden
printf alpha > /volume/z.txt`)
	treeOutput := runInVolume(volumeName, `sh -c "$1" sh "$2" "$3"`, volumeWorkspaceTreeScriptInternal, strings.Repeat("d", 5), strings.Repeat("e", 100))
	workspace, err := parseVolumeWorkspaceTreeInternal(treeOutput, 100)
	require.NoError(t, err)
	require.Equal(t, []string{"folder", "folder/nested", ".hidden", "folder/nested/child.txt", "z.txt"}, []string{
		workspace.Files[0].RelativePath,
		workspace.Files[1].RelativePath,
		workspace.Files[2].RelativePath,
		workspace.Files[3].RelativePath,
		workspace.Files[4].RelativePath,
	})
	require.True(t, workspace.Files[0].IsDirectory)
	require.True(t, workspace.Files[1].IsDirectory)
	require.False(t, workspace.Files[2].IsDirectory)
	require.False(t, workspace.Files[3].IsDirectory)
	require.False(t, workspace.Files[4].IsDirectory)

	runInVolume(volumeName, `sh -c "$1" sh nested ''`, volumeWorkspaceCreateFolderScriptInternal)
	runInVolume(volumeName, `printf one > /tmp/staged
sh -c "$1" sh nested/a.txt nested /tmp/staged 3`, volumeWorkspaceCreateFileScriptInternal)
	require.Equal(t, "644 3\n", runInVolume(volumeName, `stat -c '%a %s' /volume/nested/a.txt`))
	runInVolume(volumeName, `printf updated > /tmp/staged
sh -c "$1" sh nested/a.txt /tmp/staged 7`, volumeWorkspaceUpdateFileScriptInternal)
	require.Equal(t, "updated", runInVolume(volumeName, `head -c 7 /volume/nested/a.txt`))

	runInVolume(volumeName, `sh -c "$1" sh nested/a.txt nested/b.txt`, volumeWorkspaceRenameScriptInternal)
	runInVolume(volumeName, `sh -c "$1" sh dest ''`, volumeWorkspaceCreateFolderScriptInternal)
	runInVolume(volumeName, `sh -c "$1" sh nested/b.txt dest dest/b.txt`, volumeWorkspaceMoveScriptInternal)
	require.Equal(t, "present\x00", runInVolume(volumeName, `sh -c "$1" sh dest/b.txt`, volumeWorkspaceBackupInspectScriptInternal))

	restored := runInVolume(volumeName, `tar -cf /tmp/backup.tar -C /volume/dest ./b.txt
sh -c "$1" sh dest/b.txt 0
tar -xf /tmp/backup.tar -C /volume/dest
head -c 7 /volume/dest/b.txt`, volumeWorkspaceDeleteScriptInternal)
	require.Equal(t, "updated", restored)
}
