package workspace

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/acfs/types"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
)

func newVolumeWorkspaceTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()
	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })
	return dockerClient
}

func TestValidateVolumeHelperSupportInternal(t *testing.T) {
	var requests []string
	var options map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/volumes/workspace-volume") {
			http.Error(w, "unexpected Docker request", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(volume.Volume{Name: "workspace-volume", Driver: "local", Options: options}); err != nil {
			t.Errorf("encode volume inspect response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	dockerService := docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newVolumeWorkspaceTestDockerClientInternal(t, server))
	service := NewService(Dependencies{Docker: dockerService})

	require.NoError(t, service.validateVolumeHelperSupportInternal(t.Context(), "workspace-volume"))
	require.Equal(t, []string{"GET /v1.41/volumes/workspace-volume"}, requests)

	options = map[string]string{"type": "nfs"}
	require.NoError(t, service.validateVolumeHelperSupportInternal(t.Context(), "workspace-volume"))
	options = map[string]string{"type": "none"}
	require.Error(t, service.validateVolumeHelperSupportInternal(t.Context(), "workspace-volume"))
	options = map[string]string{"o": "bind,rw"}
	require.Error(t, service.validateVolumeHelperSupportInternal(t.Context(), "workspace-volume"))
}

func TestUpdateVolumeWorkspaceValidationFailureReturnsNoWorkspace(t *testing.T) {
	tree, err := (&Service{}).UpdateVolumeWorkspace(
		t.Context(),
		"volume",
		volumetypes.WorkspaceUpdateManifest{},
		nil,
		usertypes.Actor{},
	)

	require.Nil(t, tree)
	require.ErrorIs(t, err, common.ErrVolumeWorkspaceBadRequest)
}

func TestVolumeWorkspaceFileContentResponseInternal(t *testing.T) {
	const maxFileSizeBytes int64 = 10 * 1024 * 1024
	text := []byte("hello")
	below, err := volumeWorkspaceFileContentResponseInternal("notes.txt", "regular", maxFileSizeBytes-1, text, maxFileSizeBytes)
	require.NoError(t, err)
	require.True(t, below.Editable)
	require.Equal(t, "hello", below.Content)

	atLimit, err := volumeWorkspaceFileContentResponseInternal("notes.txt", "regular", maxFileSizeBytes, text, maxFileSizeBytes)
	require.NoError(t, err)
	require.True(t, atLimit.Editable)

	above, err := volumeWorkspaceFileContentResponseInternal("notes.txt", "regular", maxFileSizeBytes+1, text, maxFileSizeBytes)
	require.NoError(t, err)
	require.False(t, above.Editable)
	require.Equal(t, workspace.FileReadOnlyTooLarge, above.ReadOnlyReason)

	binaryContent, err := volumeWorkspaceFileContentResponseInternal("data.bin", "regular", 2, []byte{0xff, 0x00}, maxFileSizeBytes)
	require.NoError(t, err)
	require.Equal(t, workspace.FileReadOnlyBinary, binaryContent.ReadOnlyReason)

	special, err := volumeWorkspaceFileContentResponseInternal("pipe", "special", 0, nil, maxFileSizeBytes)
	require.NoError(t, err)
	require.Equal(t, workspace.FileReadOnlySpecial, special.ReadOnlyReason)
}

func TestValidateVolumeWorkspaceFileChangeInternalOperationsAndMultipartMapping(t *testing.T) {
	first := 0
	second := 1
	tests := []volumetypes.WorkspaceFileChange{
		{Operation: volumetypes.FileOpCreateFile, RelativePath: "empty.txt", UploadIndex: &first},
		{Operation: volumetypes.FileOpUpdateFile, RelativePath: "first.bin", UploadIndex: &first},
		{Operation: volumetypes.FileOpCreateFile, RelativePath: "second.bin", UploadIndex: &second},
		{Operation: volumetypes.FileOpCreateFolder, RelativePath: "folder"},
		{Operation: volumetypes.FileOpRename, RelativePath: "old.txt", NewName: "new.txt"},
		{Operation: volumetypes.FileOpMove, RelativePath: "new.txt", NewParentPath: "folder"},
		{Operation: volumetypes.FileOpDelete, RelativePath: "folder", Recursive: true},
		{Operation: volumetypes.FileOpRestoreFile, RelativePath: "restored.txt", BackupID: "backup-id"},
	}
	for _, change := range tests {
		require.NoError(t, validateVolumeWorkspaceFileChangeInternal(change), change.Operation)
	}

	require.Error(t, validateVolumeWorkspaceFileChangeInternal(volumetypes.WorkspaceFileChange{
		Operation: volumetypes.FileOpUpdateFile, RelativePath: "missing.bin",
	}))
	require.Error(t, validateVolumeWorkspaceFileChangeInternal(volumetypes.WorkspaceFileChange{
		Operation: volumetypes.FileOpRestoreFile, RelativePath: "file.txt",
	}))
	require.Error(t, validateVolumeWorkspaceFileChangeInternal(volumetypes.WorkspaceFileChange{
		Operation: "unknown", RelativePath: "file.txt",
	}))
	require.Error(t, validateVolumeWorkspaceFileChangeInternal(volumetypes.WorkspaceFileChange{
		Operation: volumetypes.FileOpDelete, RelativePath: "../escape",
	}))
}

func TestVolumeWorkspaceBackupScopeInternalCoversSourcesAndDestinations(t *testing.T) {
	changes := []volumetypes.WorkspaceFileChange{
		{Operation: volumetypes.FileOpRename, RelativePath: "docs/a.txt", NewName: "b.txt"},
		{Operation: volumetypes.FileOpMove, RelativePath: "cache", NewParentPath: "docs"},
		{Operation: volumetypes.FileOpCreateFile, RelativePath: "docs/a.txt/child", UploadIndex: new(0)},
	}
	scope, err := volumeWorkspaceBackupScopeInternal(changes)
	require.NoError(t, err)
	require.Equal(t, []string{"cache", "docs/a.txt", "docs/b.txt", "docs/cache"}, scope)
}

func TestNormalizeVolumeWorkspaceScopeInternalCollapsesAbsentAncestors(t *testing.T) {
	require.Equal(t, []string{"cache", "docs"}, normalizeVolumeWorkspaceScopeInternal([]string{
		"docs/b.txt",
		"docs",
		"cache/item",
		"cache",
		"docs/a.txt",
		"cache",
	}))
}

func TestVolumeWorkspaceRollbackPathsInternalRemovesDeepestFirst(t *testing.T) {
	backup := &volumeWorkspaceBackupInternal{
		archives: []volumeWorkspaceBackupArchiveInternal{
			{relativePath: "docs/file.txt", archivePath: "/tmp/file.tar"},
			{relativePath: "cache", archivePath: "/tmp/cache.tar"},
		},
		absentEntries: []string{"docs/new/deep", "other"},
	}
	require.Equal(t, []string{"docs/new/deep", "docs/file.txt", "other", "cache"}, volumeWorkspaceRollbackPathsInternal(backup))
}

func TestVolumeWorkspaceHelperScriptsUseSupportedTooling(t *testing.T) {
	helperScripts := []string{
		volumeWorkspaceValidatePathScriptInternal,
		volumeWorkspaceBackupCreateScriptInternal,
	}
	scripts := strings.Join(helperScripts, "\n")
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
		require.NotContains(t, scripts, unsupported)
	}
	require.Contains(t, volumeWorkspaceBackupCreateScriptInternal, `printf 'absent\0%s\0'`)
	require.Contains(t, volumeWorkspaceBackupCreateScriptInternal, `cd "$parent"`)
	require.NotContains(t, volumeWorkspaceBackupCreateScriptInternal, " -C ")
	if shellPath, err := exec.LookPath("sh"); err == nil {
		for index, script := range helperScripts {
			output, syntaxErr := exec.Command(shellPath, "-n", "-c", script).CombinedOutput()
			require.NoErrorf(t, syntaxErr, "helper script %d has invalid syntax: %s", index, output)
		}
	}
}

func TestClassifyVolumeWorkspaceExecErrorInternal(t *testing.T) {
	base := errors.New("exit status 1")
	require.ErrorIs(t, classifyVolumeWorkspaceExecErrorInternal(base, "ARCANE_SYMLINK", "execute"), common.ErrVolumeWorkspaceForbidden)
	require.ErrorIs(t, classifyVolumeWorkspaceExecErrorInternal(base, "ARCANE_NOT_FOUND", "execute"), common.ErrVolumeWorkspaceNotFound)
	require.ErrorIs(t, classifyVolumeWorkspaceExecErrorInternal(base, "ARCANE_COLLISION", "execute"), common.ErrVolumeWorkspaceBadRequest)
	require.ErrorIs(t, classifyVolumeWorkspaceExecErrorInternal(base, "unexpected", "execute"), base)
}

func TestClassifyVolumeWorkspaceHelperSupportErrorInternal(t *testing.T) {
	require.ErrorIs(t, classifyVolumeWorkspaceHelperSupportErrorInternal(errdefs.ErrNotFound), common.ErrVolumeWorkspaceNotFound)
	require.ErrorIs(t, classifyVolumeWorkspaceHelperSupportErrorInternal(errors.New("volume uses a custom mount configuration")), common.ErrVolumeWorkspaceBadRequest)
	unexpected := errors.New("docker unavailable")
	require.ErrorIs(t, classifyVolumeWorkspaceHelperSupportErrorInternal(unexpected), unexpected)
}

func TestValidateVolumeWorkspaceRevisionInternal(t *testing.T) {
	require.NoError(t, validateVolumeWorkspaceRevisionInternal(" revision ", "revision"))
	require.ErrorIs(t, validateVolumeWorkspaceRevisionInternal("stale", "current"), common.ErrVolumeWorkspaceConflict)
}

func TestStageVolumeWorkspaceChangesClearsAndCopiesContentsInOneArchive(t *testing.T) {
	var execCommands [][]string
	var copiedArchives []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/containers/helper/archive"):
			if got := r.URL.Query().Get("path"); got != "/tmp/arcane-workspace" {
				t.Errorf("unexpected archive destination %q", got)
			}
			archive := make(map[string]string)
			tarReader := tar.NewReader(r.Body)
			for {
				header, err := tarReader.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Errorf("read staging archive: %v", err)
					return
				}
				content, err := io.ReadAll(tarReader)
				if err != nil {
					t.Errorf("read staging archive content: %v", err)
					return
				}
				archive[header.Name] = string(content)
			}
			copiedArchives = append(copiedArchives, archive)
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient := newVolumeWorkspaceTestDockerClientInternal(t, server)
	service := NewService(Dependencies{
		Docker: docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(dockerClient),
		Exec: func(_ context.Context, containerID, _ string, cmd []string) (string, string, error) {
			require.Equal(t, "helper", containerID)
			execCommands = append(execCommands, cmd)
			return "", "", nil
		},
	})
	firstUpload := 0
	secondUpload := 1
	firstStaged, err := service.stageVolumeWorkspaceChangesInternal(t.Context(), dockerClient, "helper", []volumetypes.WorkspaceFileChange{
		{UploadIndex: &firstUpload},
		{},
		{UploadIndex: &secondUpload},
	}, map[int][]byte{0: []byte("alpha"), 1: {}}, volumeWorkspaceWriteIdentityInternal{})
	require.NoError(t, err)
	require.Equal(t, volumeWorkspaceStagedFileInternal{path: "/tmp/arcane-workspace/change-0", size: 5}, firstStaged[0])
	require.Equal(t, volumeWorkspaceStagedFileInternal{path: "/tmp/arcane-workspace/change-2", size: 0}, firstStaged[2])

	secondStaged, err := service.stageVolumeWorkspaceChangesInternal(
		t.Context(),
		dockerClient,
		"helper",
		[]volumetypes.WorkspaceFileChange{
			{
				UploadIndex: &firstUpload,
			},
		},
		map[int][]byte{
			0: []byte(
				"second",
			),
		},
		volumeWorkspaceWriteIdentityInternal{},
	)
	require.NoError(t, err)
	require.Equal(t, volumeWorkspaceStagedFileInternal{path: "/tmp/arcane-workspace/change-0", size: 6}, secondStaged[0])

	require.Equal(t, [][]string{
		{"sh", "-c", "rm -rf -- /tmp/arcane-workspace && mkdir -p -- /tmp/arcane-workspace"},
		{"sh", "-c", "rm -rf -- /tmp/arcane-workspace && mkdir -p -- /tmp/arcane-workspace"},
	}, execCommands)
	require.Equal(t, []map[string]string{
		{"change-0": "alpha", "change-2": ""},
		{"change-0": "second"},
	}, copiedArchives)
}

func TestVolumeWorkspaceWriteIdentityFromConfigUserInternal(t *testing.T) {
	root := volumeWorkspaceWriteIdentityInternal{}
	for _, configUser := range []string{"", "root", "0", "0:0", "0:1000", "node", "1000:node", "99999999999999999999"} {
		require.Equalf(t, root, volumeWorkspaceWriteIdentityFromConfigUserInternal(configUser), "config user %q", configUser)
	}
	require.Equal(t, volumeWorkspaceWriteIdentityInternal{execUser: "1000", uid: 1000}, volumeWorkspaceWriteIdentityFromConfigUserInternal("1000"))
	require.Equal(t, volumeWorkspaceWriteIdentityInternal{execUser: "1000:1000", uid: 1000, gid: 1000}, volumeWorkspaceWriteIdentityFromConfigUserInternal(" 1000:1000 "))
	require.Equal(t, volumeWorkspaceWriteIdentityInternal{execUser: "1000:0", uid: 1000, gid: 0}, volumeWorkspaceWriteIdentityFromConfigUserInternal("1000:0"))
}

func TestVolumeWorkspaceWritesUseRuntimeIdentity(t *testing.T) {
	type execRequestInternal struct {
		user string
		cmd  []string
	}
	var execRequests []execRequestInternal
	var archiveQueries []string
	var archiveHeaders []*tar.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/containers/helper/archive"):
			archiveQueries = append(archiveQueries, r.URL.Query().Get("copyUIDGID"))
			tarReader := tar.NewReader(r.Body)
			for {
				header, err := tarReader.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Errorf("read staging archive: %v", err)
					return
				}
				archiveHeaders = append(archiveHeaders, header)
				if _, copyErr := io.Copy(io.Discard, tarReader); copyErr != nil {
					t.Errorf("read staging archive content: %v", copyErr)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient := newVolumeWorkspaceTestDockerClientInternal(t, server)
	applyResponse, err := json.Marshal(types.ApplyResponse{Version: types.ProtocolVersion, Applied: 1})
	require.NoError(t, err)
	service := NewService(Dependencies{
		Docker: docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(dockerClient),
		Exec: func(_ context.Context, _, execUser string, cmd []string) (string, string, error) {
			execRequests = append(execRequests, execRequestInternal{user: execUser, cmd: cmd})
			return string(applyResponse), "", nil
		},
	})
	identity := volumeWorkspaceWriteIdentityFromConfigUserInternal("1000:1000")
	uploadIndex := 0
	changes := []volumetypes.WorkspaceFileChange{{Operation: volumetypes.FileOpCreateFile, RelativePath: "config.txt", UploadIndex: &uploadIndex}}
	stagedFiles, err := service.stageVolumeWorkspaceChangesInternal(t.Context(), dockerClient, "helper", changes, map[int][]byte{0: []byte("content")}, identity)
	require.NoError(t, err)
	require.NoError(t, service.executeVolumeWorkspaceACFSBatchInternal(t.Context(), dockerClient, "helper", changes, stagedFiles, 0, identity))

	require.Len(t, execRequests, 2)
	require.Empty(t, execRequests[0].user)
	require.Equal(t, "1000:1000", execRequests[1].user)
	require.Equal(t, []string{"acfs", "apply", "--root", "/volume", "--staging", "/tmp/arcane-workspace", "--manifest", "manifest-0.json"}, execRequests[1].cmd)
	require.Equal(t, []string{"true", "true"}, archiveQueries)
	require.Len(t, archiveHeaders, 2)
	for _, header := range archiveHeaders {
		require.Equal(t, 1000, header.Uid)
		require.Equal(t, 1000, header.Gid)
	}
}

func TestCreateVolumeWorkspaceMutationContainerUsesDedicatedBackupHelper(t *testing.T) {
	var createRequest struct {
		HostConfig *container.HostConfig `json:"HostConfig"`
	}
	createCalls := 0
	removeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			createCalls++
			if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
				t.Errorf("decode container create request: %v", err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "restore-helper", "Warnings": []string{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/restore-helper/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/restore-helper"):
			removeCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient := newVolumeWorkspaceTestDockerClientInternal(t, server)
	acquireCalls := 0
	service := NewService(Dependencies{
		Docker: docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(dockerClient),
		AcquireHelper: func(context.Context, string) (string, func(), error) {
			acquireCalls++
			return "cached-helper", func() {}, nil
		},
		HelperImage: func(context.Context, *client.Client) (string, error) {
			return volumehelper.ToolsImage(""), nil
		},
		BackupStorageMount: func(_ context.Context, _ *client.Client, target string, readOnly bool) (mount.Mount, error) {
			return mount.Mount{Type: mount.TypeVolume, Source: "arcane-backups", Target: target, ReadOnly: readOnly}, nil
		},
	})

	containerID, cleanup, err := service.createVolumeWorkspaceMutationContainerInternal(t.Context(), "workspace-volume", true)
	require.NoError(t, err)
	require.Equal(t, "restore-helper", containerID)
	require.Equal(t, 1, createCalls)
	require.Zero(t, acquireCalls, "the dedicated helper must not replace the shared one")
	require.NotNil(t, createRequest.HostConfig)
	require.Equal(t, []string{"workspace-volume:/volume"}, createRequest.HostConfig.Binds)
	require.Len(t, createRequest.HostConfig.Mounts, 1)
	require.Equal(t, "arcane-backups", createRequest.HostConfig.Mounts[0].Source)
	require.Equal(t, "/backups", createRequest.HostConfig.Mounts[0].Target)
	require.True(t, createRequest.HostConfig.Mounts[0].ReadOnly)

	cleanup()
	require.Equal(t, 1, removeCalls)
}

func TestVolumeWorkspaceScriptsAgainstToolsImage(t *testing.T) {
	if os.Getenv("ARCANE_VOLUME_WORKSPACE_DOCKER_TEST") != "1" {
		t.Skip("set ARCANE_VOLUME_WORKSPACE_DOCKER_TEST=1 to run the helper image integration test")
	}
	dockerPath, err := exec.LookPath("docker")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	runDocker := func(args ...string) string {
		t.Helper()
		output, combinedOutputErr := exec.CommandContext(ctx, dockerPath, args...).CombinedOutput()
		require.NoErrorf(t, combinedOutputErr, "docker %s\n%s", strings.Join(args, " "), output)
		return string(output)
	}
	runInVolume := func(volumeName, outerScript string, args ...string) string {
		t.Helper()
		dockerArgs := []string{"run", "--rm", "-v", volumeName + ":/volume", volumehelper.ToolsImage(""), "sh", "-c", outerScript, "sh"}
		dockerArgs = append(dockerArgs, args...)
		return runDocker(dockerArgs...)
	}

	volumeName := "arcane-workspace-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	runDocker("volume", "create", volumeName)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
		defer cleanupCancel()
		_, _ = exec.CommandContext(cleanupCtx, dockerPath, "volume", "rm", volumeName).CombinedOutput()
	})

	runInVolume(volumeName, `mkdir -p /volume/folder/nested
printf child > /volume/folder/nested/child.txt
printf hidden > /volume/.hidden
printf alpha > /volume/z.txt`)
	treeOutput := runInVolume(volumeName, `acfs walk --root /volume --path / --max-depth 5 --max-entries 100`)
	require.Contains(t, treeOutput, `"path":"/folder/nested/child.txt"`)
	require.Contains(t, treeOutput, `"path":"/.hidden"`)
	require.Contains(t, treeOutput, `"end":true`)

	runInVolume(volumeName, `set -e
mkdir -p /tmp/staging
printf one > /tmp/staging/change-0
printf '%s' '{"changes":[{"operation":"create_folder","path":"/nested"},{"operation":"create_file","path":"/nested/a.txt","stagedName":"change-0","size":3}],"version":2}' > /tmp/staging/manifest.json
acfs apply --root /volume --staging /tmp/staging --manifest manifest.json >/dev/null`)
	require.Equal(t, "644 3\n", runInVolume(volumeName, `stat -c '%a %s' /volume/nested/a.txt`))
	runInVolume(volumeName, `set -e
mkdir -p /tmp/staging
printf updated > /tmp/staging/change-0
printf '%s' \
  '{"changes":[{"operation":"update_file","path":"/nested/a.txt","stagedName":"change-0","size":7},{"op' \
  'eration":"rename","path":"/nested/a.txt","targetPath":"/nested/b.txt"},{"operation":"create_folder",' \
  '"path":"/dest"},{"operation":"move","path":"/nested/b.txt","targetPath":"/dest/b.txt"}],"version":2}' > /tmp/staging/manifest.json
acfs apply --root /volume --staging /tmp/staging --manifest manifest.json >/dev/null`)
	require.Equal(t, "updated", runInVolume(volumeName, `head -c 7 /volume/dest/b.txt`))

	restored := runInVolume(volumeName, `set -e
sh -c "$1" sh dest/b.txt /tmp/backup.tar >/dev/null
acfs remove --root /volume --path /dest/b.txt
tar -xf /tmp/backup.tar -C /volume/dest
head -c 7 /volume/dest/b.txt`, volumeWorkspaceBackupCreateScriptInternal)
	require.Equal(t, "updated", restored)

	runInVolume(volumeName, `mkdir -p "/volume/Test Folder"
printf spaced > "/volume/Test Folder/file.txt"`)
	spacedRestored := runInVolume(volumeName, `set -e
sh -c "$1" sh "Test Folder" /tmp/space-backup.tar >/dev/null
rm -rf "/volume/Test Folder"
tar -xf /tmp/space-backup.tar -C /volume
head -c 6 "/volume/Test Folder/file.txt"`, volumeWorkspaceBackupCreateScriptInternal)
	require.Equal(t, "spaced", spacedRestored)

	missing := runInVolume(volumeName, `set -e
sh -c "$1" sh test.txt /tmp/missing-backup.tar
if stat -c '%A' -- /tmp/missing-backup.tar >/dev/null 2>&1; then exit 1; fi`, volumeWorkspaceBackupCreateScriptInternal)
	require.Equal(t, "absent\x00test.txt\x00", missing)

	missingNested := runInVolume(volumeName, `set -e
sh -c "$1" sh "New Folder/test.txt" /tmp/missing-nested-backup.tar
if stat -c '%A' -- /tmp/missing-nested-backup.tar >/dev/null 2>&1; then exit 1; fi`, volumeWorkspaceBackupCreateScriptInternal)
	require.Equal(t, "absent\x00New Folder\x00", missingNested)
}
