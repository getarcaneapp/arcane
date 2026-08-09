package volume

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
)

func newVolumeWorkspaceTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()
	dialer := &net.Dialer{}
	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
		client.WithDialContext(func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })
	return dockerClient
}

func writeDockerExecAttachResponseInternal(t *testing.T, w http.ResponseWriter, stdout string) {
	t.Helper()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		t.Error("Docker exec response does not support hijacking")
		return
	}
	connection, buffer, err := hijacker.Hijack()
	if err != nil {
		t.Errorf("hijack Docker exec response: %v", err)
		return
	}
	defer connection.Close()
	if _, err := fmt.Fprint(buffer, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n"); err != nil {
		t.Errorf("write Docker exec response headers: %v", err)
		return
	}
	if stdout != "" {
		header := make([]byte, 8)
		header[0] = 1
		binary.BigEndian.PutUint32(header[4:], uint32(len(stdout)))
		if _, err := buffer.Write(header); err != nil {
			t.Errorf("write Docker exec stream header: %v", err)
			return
		}
		if _, err := buffer.WriteString(stdout); err != nil {
			t.Errorf("write Docker exec stream: %v", err)
			return
		}
	}
	if err := buffer.Flush(); err != nil {
		t.Errorf("flush Docker exec response: %v", err)
	}
}

func TestUpdateVolumeWorkspaceValidationFailureReturnsNoWorkspace(t *testing.T) {
	workspace, err := (&VolumeService{}).UpdateVolumeWorkspace(
		context.Background(),
		"volume",
		volumetypes.WorkspaceUpdateManifest{},
		nil,
		models.User{},
	)

	require.Nil(t, workspace)
	require.ErrorIs(t, err, common.ErrVolumeWorkspaceBadRequest)
}

func TestVolumeWorkspaceReadsWaitForMutationLock(t *testing.T) {
	testCases := map[string]func(context.Context, *VolumeService) error{
		"tree": func(ctx context.Context, service *VolumeService) error {
			_, err := service.GetVolumeWorkspace(ctx, "workspace-volume")
			return err
		},
		"file": func(ctx context.Context, service *VolumeService) error {
			_, err := service.GetVolumeWorkspaceFile(ctx, "workspace-volume", "file.txt")
			return err
		},
		"download": func(ctx context.Context, service *VolumeService) error {
			_, _, err := service.DownloadVolumeWorkspaceFile(ctx, "workspace-volume", "file.txt")
			return err
		},
	}

	for name, read := range testCases {
		t.Run(name, func(t *testing.T) {
			requestStarted := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requestStarted <- struct{}{}
				http.Error(w, "stop after lock acquisition", http.StatusInternalServerError)
			}))
			t.Cleanup(server.Close)

			service := &VolumeService{
				dockerService: docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newVolumeWorkspaceTestDockerClientInternal(t, server)),
			}
			unlockMutation := service.workspaceLocks.Lock("workspace-volume")
			readStarted := make(chan struct{})
			readDone := make(chan error, 1)
			go func() {
				close(readStarted)
				readDone <- read(context.Background(), service)
			}()
			<-readStarted

			require.Never(t, func() bool {
				select {
				case <-requestStarted:
					return true
				default:
					return false
				}
			}, 50*time.Millisecond, time.Millisecond, "workspace read reached Docker while a mutation held the volume lock")

			unlockMutation()
			require.Eventually(t, func() bool {
				select {
				case <-requestStarted:
					return true
				default:
					return false
				}
			}, time.Second, time.Millisecond)
			require.Error(t, <-readDone)
		})
	}
}

func TestDownloadVolumeWorkspaceFileHoldsReadLockUntilClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/workspace-volume"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(volume.Volume{Name: "workspace-volume", Driver: "local"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/helper/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id":    "helper",
				"State": map[string]any{"Running": true},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper/exec"):
			var request struct {
				Cmd []string `json:"Cmd"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode exec request: %v", err)
				return
			}
			wantCommand := []string{"acfs", "read", "--root", "/volume", "--path", "/file.txt"}
			if !slices.Equal(request.Cmd, wantCommand) {
				t.Errorf("unexpected read command: %v", request.Cmd)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"Id": "read-exec"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/exec/read-exec/start"):
			_, _ = io.Copy(io.Discard, r.Body)
			content := []byte("content")
			var output bytes.Buffer
			if err := acfs.WriteStreamHeader(&output, uint64(len(content))); err != nil {
				t.Errorf("write ACFS stream header: %v", err)
				return
			}
			_, _ = output.Write(content)
			writeDockerExecAttachResponseInternal(t, w, output.String())
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/exec/read-exec/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"Running": false, "ExitCode": 0})
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	service := &VolumeService{
		dockerService: docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newVolumeWorkspaceTestDockerClientInternal(t, server)),
		helperByVolume: map[string]*volumeHelper{
			"workspace-volume": {id: "helper", lastUsedAt: time.Now(), protocol: acfstypes.ProtocolVersion},
		},
	}
	reader, size, err := service.DownloadVolumeWorkspaceFile(context.Background(), "workspace-volume", "file.txt")
	require.NoError(t, err)
	require.EqualValues(t, 7, size)

	_, acquired := service.workspaceLocks.TryLock("workspace-volume")
	require.False(t, acquired, "mutation lock acquired before download stream closed")
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "content", string(content))
	require.NoError(t, reader.Close())

	unlockMutation, acquired := service.workspaceLocks.TryLock("workspace-volume")
	require.True(t, acquired, "mutation lock remained held after download stream closed")
	unlockMutation()
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
	require.Equal(t, workspacetypes.FileReadOnlyTooLarge, above.ReadOnlyReason)

	binary, err := volumeWorkspaceFileContentResponseInternal("data.bin", "regular", 2, []byte{0xff, 0x00}, maxFileSizeBytes)
	require.NoError(t, err)
	require.Equal(t, workspacetypes.FileReadOnlyBinary, binary.ReadOnlyReason)

	special, err := volumeWorkspaceFileContentResponseInternal("pipe", "special", 0, nil, maxFileSizeBytes)
	require.NoError(t, err)
	require.Equal(t, workspacetypes.FileReadOnlySpecial, special.ReadOnlyReason)
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
		restoreBackupFilesScriptInternal,
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
	require.ErrorIs(t, classifyVolumeWorkspaceHelperSupportErrorInternal(cerrdefs.ErrNotFound), common.ErrVolumeWorkspaceNotFound)
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
	execID := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper/exec"):
			execID++
			var request struct {
				Cmd []string `json:"Cmd"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode exec request: %v", err)
				return
			}
			execCommands = append(execCommands, request.Cmd)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{"Id": fmt.Sprintf("exec-%d", execID)}); err != nil {
				t.Errorf("encode exec response: %v", err)
			}
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/exec/exec-") && strings.HasSuffix(r.URL.Path, "/start"):
			_, _ = io.Copy(io.Discard, r.Body)
			writeDockerExecAttachResponseInternal(t, w, "")
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/exec/exec-") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{"Running": false, "ExitCode": 0}); err != nil {
				t.Errorf("encode exec inspect response: %v", err)
			}
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
	service := &VolumeService{dockerService: docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(dockerClient)}
	firstUpload := 0
	secondUpload := 1
	firstStaged, err := service.stageVolumeWorkspaceChangesInternal(context.Background(), dockerClient, "helper", []volumetypes.WorkspaceFileChange{
		{UploadIndex: &firstUpload},
		{},
		{UploadIndex: &secondUpload},
	}, map[int][]byte{0: []byte("alpha"), 1: {}})
	require.NoError(t, err)
	require.Equal(t, volumeWorkspaceStagedFileInternal{path: "/tmp/arcane-workspace/change-0", size: 5}, firstStaged[0])
	require.Equal(t, volumeWorkspaceStagedFileInternal{path: "/tmp/arcane-workspace/change-2", size: 0}, firstStaged[2])

	secondStaged, err := service.stageVolumeWorkspaceChangesInternal(context.Background(), dockerClient, "helper", []volumetypes.WorkspaceFileChange{{UploadIndex: &firstUpload}}, map[int][]byte{0: []byte("second")})
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

func TestUpdateVolumeWorkspaceRejectsStaleRevisionBeforeStaging(t *testing.T) {
	var execCommands [][]string
	archiveCopies := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/workspace-volume"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(volume.Volume{Name: "workspace-volume", Driver: "local"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"Id": "tools-image"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "helper", "Warnings": []string{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper/exec"):
			var request struct {
				Cmd []string `json:"Cmd"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode exec request: %v", err)
				return
			}
			execCommands = append(execCommands, request.Cmd)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"Id": fmt.Sprintf("workspace-exec-%d", len(execCommands)-1)})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/exec/workspace-exec-") && strings.HasSuffix(r.URL.Path, "/start"):
			_, _ = io.Copy(io.Discard, r.Body)
			output := "{\"version\":\"0.2.0\",\"revision\":\"test\",\"buildTime\":\"test\",\"protocol\":2}\n"
			if strings.Contains(r.URL.Path, "workspace-exec-1") {
				output = "{\"end\":true,\"version\":2}\n"
			}
			writeDockerExecAttachResponseInternal(t, w, output)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/exec/workspace-exec-") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"Running": false, "ExitCode": 0})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/containers/helper/archive"):
			archiveCopies++
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient := newVolumeWorkspaceTestDockerClientInternal(t, server)
	service := &VolumeService{
		dockerService:  docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(dockerClient),
		helperByVolume: make(map[string]*volumeHelper),
	}
	uploadIndex := 0
	workspace, err := service.UpdateVolumeWorkspace(context.Background(), "workspace-volume", volumetypes.WorkspaceUpdateManifest{
		FileTreeRevision: "stale",
		FileChanges: []volumetypes.WorkspaceFileChange{{
			Operation:    volumetypes.FileOpCreateFile,
			RelativePath: "new.txt",
			UploadIndex:  &uploadIndex,
		}},
	}, map[int][]byte{0: []byte("new content")}, models.User{})

	require.Nil(t, workspace)
	require.ErrorIs(t, err, common.ErrVolumeWorkspaceConflict)
	require.Len(t, execCommands, 2)
	require.Equal(t, []string{"acfs", "version"}, execCommands[0])
	require.Equal(t, []string{"acfs", "walk", "--root", "/volume", "--path", "/", "--max-depth", "50", "--max-entries", "10000"}, execCommands[1])
	require.Zero(t, archiveCopies)
}

func TestCreateVolumeWorkspaceMutationContainerUsesDedicatedBackupHelper(t *testing.T) {
	var createRequest struct {
		HostConfig *container.HostConfig `json:"HostConfig"`
	}
	createCalls := 0
	removeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"Id": "tools-image"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]container.Summary{})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/arcane-backups"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(volume.Volume{Name: "arcane-backups", Driver: "local"})
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
	service := &VolumeService{
		dockerService:    docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(dockerClient),
		backupVolumeName: "arcane-backups",
		helperByVolume: map[string]*volumeHelper{
			"workspace-volume": {id: "cached-helper", lastUsedAt: time.Now()},
		},
	}

	containerID, cleanup, err := service.createVolumeWorkspaceMutationContainerInternal(context.Background(), "workspace-volume", true)
	require.NoError(t, err)
	require.Equal(t, "restore-helper", containerID)
	require.Equal(t, 1, createCalls)
	require.Equal(t, "cached-helper", service.helperByVolume["workspace-volume"].id)
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
printf '%s' '{"changes":[{"operation":"update_file","path":"/nested/a.txt","stagedName":"change-0","size":7},{"operation":"rename","path":"/nested/a.txt","targetPath":"/nested/b.txt"},{"operation":"create_folder","path":"/dest"},{"operation":"move","path":"/nested/b.txt","targetPath":"/dest/b.txt"}],"version":2}' > /tmp/staging/manifest.json
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
