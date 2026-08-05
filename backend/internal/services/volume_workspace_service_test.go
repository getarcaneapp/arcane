package services

import (
	"archive/tar"
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
	"strconv"
	"strings"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
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

func volumeWorkspaceTreeRecordInternal(relativePath, kind, size, modTime, mode, linkTarget string) string {
	return strings.Join([]string{relativePath, kind, size, modTime, mode, linkTarget}, "\x00") + "\x00"
}

func volumeWorkspaceTreeOutputInternal(truncated bool, records string) string {
	marker := "0"
	if truncated {
		marker = "1"
	}
	return records + "ARCANE_TREE_END\x00" + marker + "\x00"
}

func TestParseVolumeWorkspaceTreeInternalSortsAndClassifiesFromMode(t *testing.T) {
	stdout := volumeWorkspaceTreeOutputInternal(false,
		volumeWorkspaceTreeRecordInternal("z.txt", "d", "4", "1700000000.25", "-rw-r--r--", "")+
			volumeWorkspaceTreeRecordInternal("folder/link", "f", "7", "1700000001.5", "lrwxrwxrwx", "../z.txt")+
			volumeWorkspaceTreeRecordInternal("folder/child.txt", "d", "5", "1700000001.75", "-rw-r--r--", "")+
			volumeWorkspaceTreeRecordInternal("folder", "f", "4096", "1700000002", "drwxr-xr-x", "")+
			volumeWorkspaceTreeRecordInternal("empty", "f", "4096", "1700000003", "drwx------", "")+
			volumeWorkspaceTreeRecordInternal("special", "f", "0", "1700000004", "prw-------", ""))

	workspace, err := parseVolumeWorkspaceTreeInternal(stdout, 10)
	require.NoError(t, err)
	require.False(t, workspace.FileTreeTruncated)
	require.Equal(t, []string{"empty", "folder", "folder/child.txt", "folder/link", "special", "z.txt"}, []string{
		workspace.Files[0].RelativePath,
		workspace.Files[1].RelativePath,
		workspace.Files[2].RelativePath,
		workspace.Files[3].RelativePath,
		workspace.Files[4].RelativePath,
		workspace.Files[5].RelativePath,
	})
	require.Zero(t, workspace.Files[0].Size)
	require.Zero(t, workspace.Files[1].Size)
	require.True(t, workspace.Files[0].IsDirectory)
	require.True(t, workspace.Files[1].IsDirectory)
	require.False(t, workspace.Files[2].IsDirectory)
	require.False(t, workspace.Files[3].IsDirectory)
	require.False(t, workspace.Files[4].IsDirectory)
	require.False(t, workspace.Files[5].IsDirectory)
	require.True(t, workspace.Files[3].IsSymlink)
	require.Equal(t, "../z.txt", workspace.Files[3].LinkTarget)
	require.NotEmpty(t, workspace.FileTreeRevision)

	reordered, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(false,
		volumeWorkspaceTreeRecordInternal("empty", "d", "4096", "1700000003", "drwx------", "")+
			volumeWorkspaceTreeRecordInternal("folder", "d", "4096", "1700000002", "drwxr-xr-x", "")+
			volumeWorkspaceTreeRecordInternal("folder/child.txt", "f", "5", "1700000001.75", "-rw-r--r--", "")+
			volumeWorkspaceTreeRecordInternal("z.txt", "f", "4", "1700000000.25", "-rw-r--r--", "")+
			volumeWorkspaceTreeRecordInternal("special", "s", "0", "1700000004", "prw-------", "")+
			volumeWorkspaceTreeRecordInternal("folder/link", "l", "7", "1700000001.5", "lrwxrwxrwx", "../z.txt")), 10)
	require.NoError(t, err)
	require.Equal(t, workspace.FileTreeRevision, reordered.FileTreeRevision)
}

func TestParseVolumeWorkspaceTreeInternalFallsBackToKindWithoutMode(t *testing.T) {
	workspace, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(false,
		volumeWorkspaceTreeRecordInternal("folder", "d", "4096", "1", "", "")+
			volumeWorkspaceTreeRecordInternal("link", "l", "3", "2", "", "folder")+
			volumeWorkspaceTreeRecordInternal("regular", "f", "4", "3", "", "")), 10)
	require.NoError(t, err)
	require.True(t, workspace.Files[0].IsDirectory)
	require.Zero(t, workspace.Files[0].Size)
	require.True(t, workspace.Files[1].IsSymlink)

	special, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(false,
		volumeWorkspaceTreeRecordInternal("folder", "d", "4096", "1", "", "")+
			volumeWorkspaceTreeRecordInternal("link", "l", "3", "2", "", "folder")+
			volumeWorkspaceTreeRecordInternal("regular", "s", "4", "3", "", "")), 10)
	require.NoError(t, err)
	require.NotEqual(t, workspace.FileTreeRevision, special.FileTreeRevision)
}

func TestParseVolumeWorkspaceTreeInternalReportsBothLimits(t *testing.T) {
	entries := volumeWorkspaceTreeRecordInternal("b", "f", "1", "1", "-rw-r--r--", "") +
		volumeWorkspaceTreeRecordInternal("a", "f", "1", "1", "-rw-r--r--", "")

	depthLimited, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(true, entries), 10)
	require.NoError(t, err)
	require.True(t, depthLimited.FileTreeTruncated)

	entryLimited, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(false, entries), 1)
	require.NoError(t, err)
	require.True(t, entryLimited.FileTreeTruncated)
	require.Len(t, entryLimited.Files, 1)
	require.Equal(t, "a", entryLimited.Files[0].RelativePath)

	_, err = parseVolumeWorkspaceTreeInternal(entries+"invalid\x000\x00", 10)
	require.Error(t, err)
}

func TestParseVolumeWorkspaceModTimeInternalPreservesNanoseconds(t *testing.T) {
	modTime, err := parseVolumeWorkspaceModTimeInternal("2026-08-02 16:09:00.123456789 +0000")
	require.NoError(t, err)
	require.Equal(t, 123456789, modTime.Nanosecond())
}

func TestParseVolumeWorkspaceTreeInternalPreservesUnusualNamesAndSpecialFiles(t *testing.T) {
	workspace, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(false,
		volumeWorkspaceTreeRecordInternal(".hidden", "f", "1", "1", "-rw-r--r--", "")+
			volumeWorkspaceTreeRecordInternal("line\nbreak", "s", "0", "2", "prw-------", "")), 10)
	require.NoError(t, err)
	require.Equal(t, []string{".hidden", "line\nbreak"}, []string{
		workspace.Files[0].RelativePath,
		workspace.Files[1].RelativePath,
	})
	require.NotEmpty(t, workspace.FileTreeRevision)
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

func TestValidateVolumeDownloadHeaderInternalRejectsUnsafeEntries(t *testing.T) {
	require.NoError(t, validateVolumeDownloadHeaderInternal(&tar.Header{Typeflag: tar.TypeReg, Mode: 0o644}))
	require.EqualError(t, validateVolumeDownloadHeaderInternal(&tar.Header{Typeflag: tar.TypeDir, Mode: 0o755}), "path is a directory")
	require.NoError(t, validateVolumeDownloadHeaderInternal(&tar.Header{Typeflag: tar.TypeSymlink, Mode: 0o777}))
	require.NoError(t, validateVolumeDownloadHeaderInternal(&tar.Header{Typeflag: tar.TypeFifo, Mode: 0o644}))
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
		volumeWorkspaceTreeScriptInternal,
		volumeWorkspaceValidatePathScriptInternal,
		volumeWorkspaceBackupCreateScriptInternal,
		volumeWorkspaceCreateFileScriptInternal,
		volumeWorkspaceCreateFolderScriptInternal,
		volumeWorkspaceUpdateFileScriptInternal,
		volumeWorkspaceRenameScriptInternal,
		volumeWorkspaceMoveScriptInternal,
		volumeWorkspaceDeleteScriptInternal,
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
	require.Contains(t, volumeWorkspaceCreateFileScriptInternal, "head -c")
	require.Contains(t, volumeWorkspaceCreateFolderScriptInternal, "mkdir -m 0755")
	require.Contains(t, volumeWorkspaceUpdateFileScriptInternal, "head -c")
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
	service := &VolumeService{dockerService: &DockerClientService{client: dockerClient}}
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
			_ = json.NewEncoder(w).Encode(map[string]string{"Id": "tree-exec"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/exec/tree-exec/start"):
			_, _ = io.Copy(io.Discard, r.Body)
			writeDockerExecAttachResponseInternal(t, w, volumeWorkspaceTreeOutputInternal(false, ""))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/exec/tree-exec/json"):
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
		dockerService:  &DockerClientService{client: dockerClient},
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
	require.Len(t, execCommands, 1)
	require.Contains(t, execCommands[0], volumeWorkspaceTreeScriptInternal)
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

	dockerClient := newTestDockerClient(t, server)
	service := &VolumeService{
		dockerService:    &DockerClientService{client: dockerClient},
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

func TestApplyVolumeWorkspaceChangesTransactionInternalRollsBackEveryMutationStage(t *testing.T) {
	changes := []volumetypes.WorkspaceFileChange{
		{Operation: volumetypes.FileOpCreateFolder, RelativePath: "one"},
		{Operation: volumetypes.FileOpRename, RelativePath: "one", NewName: "two"},
		{Operation: volumetypes.FileOpDelete, RelativePath: "two", Recursive: true},
	}
	applyFailure := errors.New("apply failed")
	for failureIndex := range changes {
		t.Run(changes[failureIndex].Operation, func(t *testing.T) {
			applied := make([]int, 0, failureIndex+1)
			rollbackCalls := 0
			err := applyVolumeWorkspaceChangesTransactionInternal(changes, func(index int, _ volumetypes.WorkspaceFileChange) error {
				applied = append(applied, index)
				if index == failureIndex {
					return applyFailure
				}
				return nil
			}, func() error {
				rollbackCalls++
				return nil
			})
			require.ErrorIs(t, err, applyFailure)
			require.Equal(t, failureIndex+1, len(applied))
			require.Equal(t, 1, rollbackCalls)
		})
	}

	rollbackFailure := errors.New("rollback failed")
	err := applyVolumeWorkspaceChangesTransactionInternal(changes, func(_ int, _ volumetypes.WorkspaceFileChange) error {
		return applyFailure
	}, func() error {
		return rollbackFailure
	})
	require.ErrorIs(t, err, applyFailure)
	require.ErrorIs(t, err, rollbackFailure)
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

	runInVolume(volumeName, `sh -c "$1" sh folder/nested/child.txt 0`, volumeWorkspaceValidatePathScriptInternal)
	runInVolume(volumeName, `sh -c "$1" sh missing/path 1`, volumeWorkspaceValidatePathScriptInternal)
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

	restored := runInVolume(volumeName, `set -e
sh -c "$1" sh dest/b.txt /tmp/backup.tar >/dev/null
sh -c "$2" sh dest/b.txt 0
tar -xf /tmp/backup.tar -C /volume/dest
head -c 7 /volume/dest/b.txt`, volumeWorkspaceBackupCreateScriptInternal, volumeWorkspaceDeleteScriptInternal)
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
