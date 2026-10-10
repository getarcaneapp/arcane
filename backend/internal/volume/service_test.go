package volume

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/user"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/acfs"
	"go.getarcane.app/acfs/types"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
)

func TestEnrichVolumesWithUsageDataInternal(t *testing.T) {
	tests := []struct {
		name         string
		volumes      []volume.Volume
		usageVolumes []volume.Volume
		wantLen      int
		assertions   func(t *testing.T, got []volume.Volume)
	}{
		{
			name: "attaches usage by name",
			volumes: []volume.Volume{
				{Name: "vol-a"},
				{Name: "vol-b"},
			},
			usageVolumes: []volume.Volume{
				{Name: "vol-a", UsageData: &volume.UsageData{Size: 100, RefCount: 2}},
				{Name: "vol-c", UsageData: &volume.UsageData{Size: 50, RefCount: 1}},
			},
			wantLen: 2,
			assertions: func(t *testing.T, got []volume.Volume) {
				require.NotNil(t, got[0].UsageData)
				require.EqualValues(t, 100, got[0].UsageData.Size)
				require.EqualValues(t, 2, got[0].UsageData.RefCount)
				require.Nil(t, got[1].UsageData)
			},
		},
		{
			name: "keeps first usage entry when duplicate usage names exist",
			volumes: []volume.Volume{
				{Name: "vol-dup"},
			},
			usageVolumes: []volume.Volume{
				{Name: "vol-dup", UsageData: &volume.UsageData{Size: 10, RefCount: 1}},
				{Name: "vol-dup", UsageData: &volume.UsageData{Size: 20, RefCount: 3}},
			},
			wantLen: 1,
			assertions: func(t *testing.T, got []volume.Volume) {
				require.NotNil(t, got[0].UsageData)
				require.EqualValues(t, 10, got[0].UsageData.Size)
				require.EqualValues(t, 1, got[0].UsageData.RefCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := enrichVolumesWithUsageDataInternal(tt.volumes, tt.usageVolumes)
			require.Len(t, got, tt.wantLen)
			tt.assertions(t, got)
		})
	}
}

func TestIsInternalVolumeInternal(t *testing.T) {
	svc := &VolumeService{backupVolumeName: "arcane-backups"}

	require.True(t, svc.isInternalVolumeInternal(volumetypes.Volume{Name: "arcane-backups"}))
	require.False(t, svc.isInternalVolumeInternal(volumetypes.Volume{Name: "user-volume"}))
}

func TestRenameVolumeRejectsInvalidAndProtectedNames(t *testing.T) {
	service := &VolumeService{backupVolumeName: "arcane-backups"}

	_, err := service.RenameVolume(t.Context(), "data", "data", user.Actor{})
	require.ErrorIs(t, err, common.ErrVolumeRenameInvalid)

	_, err = service.RenameVolume(t.Context(), "arcane-backups", "renamed-backups", user.Actor{})
	require.ErrorIs(t, err, common.ErrVolumeRenameProtected)

	_, err = service.RenameVolume(t.Context(), "data", "arcane-backups", user.Actor{})
	require.ErrorIs(t, err, common.ErrVolumeRenameProtected)
}

func applyVolumeBackupMigrationsInternal(t *testing.T, gormDB *gorm.DB) {
	t.Helper()
	for _, name := range []string{"032_add_volume_backups.sql", "073_add_backup_support.sql", "087_add_volume_backup_remote_instance.sql"} {
		migration, err := os.ReadFile("../../resources/migrations/sqlite/" + name)
		require.NoError(t, err)
		require.NoError(t, gormDB.Exec(strings.Split(string(migration), "-- +goose Down")[0]).Error)
	}
}

func TestRenameVolumeMetadataInternalPreservesPoliciesAndHistory(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-rename-metadata?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	applyVolumeBackupMigrationsInternal(t, gormDB)
	require.NoError(t, gormDB.Exec("INSERT INTO volume_backup_policies (id, volume_name, schedule) VALUES (?, ?, ?)", "policy-1", "source-data", "0 0 2 * * *").Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{VolumeName: "source-data", PolicyID: "policy-1"}).Error)

	service := NewVolumeService(&database.DB{DB: gormDB}, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, service.renameVolumeMetadataInternal(t.Context(), "source-data", "renamed-data"))

	var renamedPolicy string
	require.NoError(t, gormDB.Raw("SELECT volume_name FROM volume_backup_policies WHERE id = ?", "policy-1").Scan(&renamedPolicy).Error)
	require.Equal(t, "renamed-data", renamedPolicy)

	var backupEntry VolumeBackup
	require.NoError(t, gormDB.Where("policy_id = ?", "policy-1").First(&backupEntry).Error)
	require.Equal(t, "renamed-data", backupEntry.VolumeName)
}

func TestBuildVolumePruneOptionsInternal_PreservesInternalVolumes(t *testing.T) {
	options := buildVolumePruneOptionsInternal(true)

	require.True(t, options.All)
	require.NotNil(t, options.Filters)
	require.True(t, options.Filters["label!"][internalVolumePruneFilterValue])
}

func TestBuildVolumePruneOptionsInternal_PreservesInternalVolumesForAnonymousVolumes(t *testing.T) {
	options := buildVolumePruneOptionsInternal(false)

	require.False(t, options.All)
	require.NotNil(t, options.Filters)
	require.True(t, options.Filters["label!"][internalVolumePruneFilterValue])
}

func TestBuildVolumePruneMetadataInternal(t *testing.T) {
	metadata := buildVolumePruneMetadataInternal(true, 2, 4096)

	require.Equal(t, "prune", metadata["action"])
	require.Equal(t, true, metadata["all"])
	require.Equal(t, 2, metadata["volumesDeleted"])
	require.EqualValues(t, 4096, metadata["spaceReclaimed"])
	require.Equal(t, internalVolumePruneFilterValue, metadata["internalVolumeFilterLabel"])
}

func TestListBackupsPaginatedByManagementTypeInternal(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-management?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackup{}))
	for _, entry := range []*VolumeBackup{
		{VolumeName: "app", Status: VolumeBackupStatusSucceeded, Destination: volumetypes.BackupDestinationLocal, PolicyID: backup.SystemVolumePolicyPrefix + "abc"},
		{VolumeName: "app", Status: VolumeBackupStatusSucceeded, Destination: volumetypes.BackupDestinationLocal, PolicyID: "per-volume"},
		{VolumeName: "app", Status: VolumeBackupStatusSucceeded, Destination: volumetypes.BackupDestinationLocal},
	} {
		require.NoError(t, gormDB.Create(entry).Error)
	}
	service := &VolumeService{db: &database.DB{DB: gormDB}}

	systemRows, total, err := service.listBackupsInternal(t.Context(), "app", pagination.QueryParams{
		Filters: map[string]string{"type": "system"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, systemRows, 1)
	require.Equal(t, backup.ManagementTypeSystem, systemRows[0].Type)

	volumeRows, total, err := service.listBackupsInternal(t.Context(), "app", pagination.QueryParams{
		Filters: map[string]string{"type": "volume"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, volumeRows, 2)
	for _, entry := range volumeRows {
		require.Equal(t, backup.ManagementTypeVolume, entry.Type)
	}
}

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
	defer func() { _ = connection.Close() }()
	if _, fprintErr := fmt.Fprint(buffer, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n"); fprintErr != nil {
		t.Errorf("write Docker exec response headers: %v", fprintErr)
		return
	}
	if stdout != "" {
		header := make([]byte, 8)
		header[0] = 1
		binary.BigEndian.PutUint32(header[4:], uint32(len(stdout)))
		if _, writeErr := buffer.Write(header); writeErr != nil {
			t.Errorf("write Docker exec stream header: %v", writeErr)
			return
		}
		if _, writeStringErr := buffer.WriteString(stdout); writeStringErr != nil {
			t.Errorf("write Docker exec stream: %v", writeStringErr)
			return
		}
	}
	if flushErr := buffer.Flush(); flushErr != nil {
		t.Errorf("flush Docker exec response: %v", flushErr)
	}
}

func TestVolumeWorkspaceReadsWaitForMutationLock(t *testing.T) {
	testCases := map[string]func(context.Context, *VolumeService) error{
		"tree": func(ctx context.Context, service *VolumeService) error {
			_, err := service.workspace.GetVolumeWorkspace(ctx, "workspace-volume")
			return err
		},
		"file": func(ctx context.Context, service *VolumeService) error {
			_, err := service.workspace.GetVolumeWorkspaceFile(ctx, "workspace-volume", "file.txt")
			return err
		},
		"download": func(ctx context.Context, service *VolumeService) error {
			_, _, err := service.workspace.DownloadVolumeWorkspaceFile(ctx, "workspace-volume", "file.txt")
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

			dockerService := docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newVolumeWorkspaceTestDockerClientInternal(t, server))
			service := NewVolumeService(nil, dockerService, nil, nil, nil, nil, nil, nil, nil, nil)
			unlockMutation := service.workspaceLocks.Lock("workspace-volume")
			readStarted := make(chan struct{})
			readDone := make(chan error, 1)
			go func() {
				close(readStarted)
				readDone <- read(t.Context(), service)
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

	dockerService := docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newVolumeWorkspaceTestDockerClientInternal(t, server))
	service := NewVolumeService(nil, dockerService, nil, nil, nil, nil, nil, nil, nil, nil)
	service.helperByVolume["workspace-volume"] = &volumeHelper{id: "helper", lastUsedAt: time.Now(), protocol: types.ProtocolVersion}
	reader, size, err := service.workspace.DownloadVolumeWorkspaceFile(t.Context(), "workspace-volume", "file.txt")
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
			output := kit.Ternary(
				strings.Contains(r.URL.Path, "workspace-exec-1"),
				"{\"end\":true,\"version\":2}\n",
				"{\"version\":\"0.2.0\",\"revision\":\"test\",\"buildTime\":\"test\",\"protocol\":2}\n",
			)
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
	service := NewVolumeService(nil, docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(dockerClient), nil, nil, nil, nil, nil, nil, nil, nil)
	uploadIndex := 0
	workspace, err := service.workspace.UpdateVolumeWorkspace(t.Context(), "workspace-volume", volumetypes.WorkspaceUpdateManifest{
		FileTreeRevision: "stale",
		FileChanges: []volumetypes.WorkspaceFileChange{{
			Operation:    volumetypes.FileOpCreateFile,
			RelativePath: "new.txt",
			UploadIndex:  &uploadIndex,
		}},
	}, map[int][]byte{0: []byte("new content")}, user.Actor{})

	require.Nil(t, workspace)
	require.ErrorIs(t, err, common.ErrVolumeWorkspaceConflict)
	require.Len(t, execCommands, 2)
	require.Equal(t, []string{"acfs", "version"}, execCommands[0])
	require.Equal(t, []string{"acfs", "walk", "--root", "/volume", "--path", "/", "--max-depth", "50", "--max-entries", "10000"}, execCommands[1])
	require.Zero(t, archiveCopies)
}

func TestVolumeBackup_ListResolvesDestinationName(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-destination-name?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackup{}, &s3.S3Destination{}))
	db := &database.DB{DB: gormDB}
	destination := &s3.S3Destination{
		Name:            "Offsite",
		Bucket:          "volume-backups",
		Region:          "us-east-1",
		AccessKeyID:     "access-key",
		SecretAccessKey: "encrypted-secret",
	}
	require.NoError(t, gormDB.Create(destination).Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{
		VolumeName:      "app-data",
		Status:          VolumeBackupStatusSucceeded,
		Trigger:         VolumeBackupTriggerManual,
		Destination:     volumetypes.BackupDestinationLocalS3,
		S3DestinationID: destination.ID,
	}).Error)

	service := NewVolumeService(db, nil, nil, nil, nil, nil, nil, s3.NewS3DestinationService(db, nil), nil, nil)
	backups, _, err := service.backup.List(t.Context(), "app-data", pagination.QueryParams{})
	require.NoError(t, err)
	require.Len(t, backups, 1)
	require.Equal(t, volumetypes.BackupDestinationLocalS3, backups[0].Destination)
	require.Equal(t, "Offsite", backups[0].S3DestinationName)
}

func TestVolumeBackupPolicy_GetReturnsLastRunForEachPolicyFromStore(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-last-runs-store?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	applyVolumeBackupMigrationsInternal(t, gormDB)
	require.NoError(t, gormDB.Create(&VolumeBackup{VolumeName: "app-data", PolicyID: "first-policy", Status: VolumeBackupStatusSucceeded}).Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{VolumeName: "app-data", PolicyID: "second-policy", Status: VolumeBackupStatusFailed}).Error)

	store := NewVolumeService(&database.DB{DB: gormDB}, nil, nil, nil, nil, nil, nil, nil, nil, nil).backupStoreInternal()
	first, err := store.Latest(t.Context(), "first-policy")
	require.NoError(t, err)
	require.Equal(t, "succeeded", first.Status)
	second, err := store.Latest(t.Context(), "second-policy")
	require.NoError(t, err)
	require.Equal(t, "failed", second.Status)
}
