package restore

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/types/v3"
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

func TestRestoreFileExtractsLegacyArchiveMember(t *testing.T) {
	var commands [][]string
	service := NewService(Dependencies{
		Run: func(_ context.Context, backupID string) (*types.VolumeBackup, error) {
			return &types.VolumeBackup{ID: backupID, VolumeName: "app-data", Format: types.VolumeBackupFormatArchive}, nil
		},
		ArchiveFilename: func(backupID string) (string, error) { return backupID + ".tar.gz", nil },
		Exec: func(_ context.Context, _, _ string, cmd []string) (string, string, error) {
			commands = append(commands, cmd)
			return "", "", nil
		},
	})

	_, err := service.RestoreFile(t.Context(), types.VolumeBackupFileRestoreOptions{ContainerID: "helper", VolumeName: "other-volume", BackupID: "backup-1", Path: "docs/app.conf"})
	require.ErrorIs(t, err, common.ErrVolumeWorkspaceForbidden)
	require.Empty(t, commands)

	_, err = service.RestoreFile(t.Context(), types.VolumeBackupFileRestoreOptions{ContainerID: "helper", VolumeName: "app-data", BackupID: "backup-1", Path: "/docs/app.conf"})
	require.NoError(t, err)
	require.Equal(t, [][]string{{"sh", "-c", restoreBackupFilesScript, "sh", "/backups/backup-1.tar.gz", "./docs/app.conf"}}, commands)
}

func TestRestoreFileRestoresRusticSnapshotPath(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", filepath.Join(t.TempDir(), "missing.pem"))
	t.Setenv("SSL_CERT_DIR", t.TempDir())
	var createRequest struct {
		HostConfig *container.HostConfig `json:"HostConfig"`
		Cmd        []string              `json:"Cmd"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"sha256:rustic"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/volumes/create"):
			_, _ = w.Write([]byte(`{"Name":"rustic-cache"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			if err := json.UnmarshalRead(r.Body, &createRequest); err != nil {
				t.Errorf("decode container create request: %v", err)
			}
			http.Error(w, "stop after create", http.StatusInternalServerError)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"), client.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	service := NewService(Dependencies{
		Docker: (&docker.DockerClientService{}).WithClient(dockerClient),
		Engine: backup.NewEngine(nil, nil, nil),
		Run: func(_ context.Context, backupID string) (*types.VolumeBackup, error) {
			return &types.VolumeBackup{ID: backupID, VolumeName: "app-data", Format: types.VolumeBackupFormatRustic, LocalSnapshotID: "snapshot-1"}, nil
		},
		Repository: func(_ context.Context, _ *client.Client, entry *types.VolumeBackup) (backup.Repository, string, error) {
			return backup.Repository{ID: "local"}, entry.LocalSnapshotID, nil
		},
		Password: func(context.Context, *client.Client, ...backup.Repository) (string, error) { return "secret", nil },
		Exec: func(context.Context, string, string, []string) (string, string, error) {
			t.Fatal("Rustic restores must not extract a legacy archive")
			return "", "", nil
		},
	})

	_, err = service.RestoreFile(t.Context(), types.VolumeBackupFileRestoreOptions{ContainerID: "helper", VolumeName: "app-data", BackupID: "backup-1", Path: "docs/app.conf"})
	require.ErrorContains(t, err, "stop after create")
	require.Equal(t, []string{"restore", "--verify-existing", "--", "snapshot-1:/docs/app.conf", "/volume/docs/app.conf"}, createRequest.Cmd)
	require.NotNil(t, createRequest.HostConfig)
	require.Contains(t, createRequest.HostConfig.Mounts, mount.Mount{Type: mount.TypeVolume, Source: "app-data", Target: "/volume"})
}
