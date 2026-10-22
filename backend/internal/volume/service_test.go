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
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

func TestEnrichVolumesWithUsageData(t *testing.T) {
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
			got := enrichVolumesWithUsageData(tt.volumes, tt.usageVolumes)
			require.Len(t, got, tt.wantLen)
			tt.assertions(t, got)
		})
	}
}

func TestIsInternalVolume(t *testing.T) {
	svc := &VolumeService{backupVolumeName: "arcane-backups"}

	require.True(t, svc.isInternalVolume(types.Volume{Name: "arcane-backups"}))
	require.False(t, svc.isInternalVolume(types.Volume{Name: "user-volume"}))
}

func TestRenameVolumeRejectsInvalidAndProtectedNames(t *testing.T) {
	service := &VolumeService{backupVolumeName: "arcane-backups"}

	_, err := service.RenameVolume(t.Context(), "data", "data", types.Actor{})
	require.ErrorIs(t, err, common.ErrVolumeRenameInvalid)

	_, err = service.RenameVolume(t.Context(), "arcane-backups", "renamed-backups", types.Actor{})
	require.ErrorIs(t, err, common.ErrVolumeRenameProtected)

	_, err = service.RenameVolume(t.Context(), "data", "arcane-backups", types.Actor{})
	require.ErrorIs(t, err, common.ErrVolumeRenameProtected)
}

func applyVolumeBackupMigrations(t *testing.T, gormDB *gorm.DB) {
	t.Helper()
	for _, name := range []string{"032_add_volume_backups.sql", "073_add_backup_support.sql", "087_add_volume_backup_remote_instance.sql"} {
		migration, err := os.ReadFile("../../resources/migrations/sqlite/" + name)
		require.NoError(t, err)
		require.NoError(t, gormDB.Exec(strings.Split(string(migration), "-- +goose Down")[0]).Error)
	}
}

func TestRenameVolumeMetadataPreservesPoliciesAndHistory(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-rename-metadata?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	applyVolumeBackupMigrations(t, gormDB)
	require.NoError(t, gormDB.Exec("INSERT INTO volume_backup_policies (id, volume_name, schedule) VALUES (?, ?, ?)", "policy-1", "source-data", "0 0 2 * * *").Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{VolumeName: "source-data", PolicyID: "policy-1"}).Error)

	service := NewVolumeService(&database.DB{DB: gormDB}, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, service.renameVolumeMetadata(t.Context(), "source-data", "renamed-data"))

	var renamedPolicy string
	require.NoError(t, gormDB.Raw("SELECT volume_name FROM volume_backup_policies WHERE id = ?", "policy-1").Scan(&renamedPolicy).Error)
	require.Equal(t, "renamed-data", renamedPolicy)

	var backupEntry VolumeBackup
	require.NoError(t, gormDB.Where("policy_id = ?", "policy-1").First(&backupEntry).Error)
	require.Equal(t, "renamed-data", backupEntry.VolumeName)
}

func TestListBackupsPaginatedByManagementType(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-management?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackup{}))
	for _, entry := range []*VolumeBackup{
		{VolumeName: "app", Status: VolumeBackupStatusSucceeded, Destination: types.VolumeBackupDestinationLocal, PolicyID: types.SystemVolumePolicyPrefix + "abc"},
		{VolumeName: "app", Status: VolumeBackupStatusSucceeded, Destination: types.VolumeBackupDestinationLocal, PolicyID: "per-volume"},
		{VolumeName: "app", Status: VolumeBackupStatusSucceeded, Destination: types.VolumeBackupDestinationLocal},
	} {
		require.NoError(t, gormDB.Create(entry).Error)
	}
	service := &VolumeService{db: &database.DB{DB: gormDB}}

	systemRows, page, err := service.listBackups(t.Context(), "app", pagination.QueryParams{
		Filters: map[string]string{"type": "system"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, systemRows, 1)
	require.Equal(t, types.BackupManagementTypeSystem, systemRows[0].Type)

	volumeRows, page, err := service.listBackups(t.Context(), "app", pagination.QueryParams{
		Filters: map[string]string{"type": "volume"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, volumeRows, 2)
	for _, entry := range volumeRows {
		require.Equal(t, types.BackupManagementTypeVolume, entry.Type)
	}
}

func newVolumeWorkspaceTestDockerClient(t *testing.T, server *httptest.Server) *client.Client {
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

func writeDockerExecAttachResponse(t *testing.T, w http.ResponseWriter, stdout string) {
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

			dockerService := (&docker.DockerClientService{}).WithClient(newVolumeWorkspaceTestDockerClient(t, server))
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
			writeDockerExecAttachResponse(t, w, output.String())
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/exec/read-exec/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"Running": false, "ExitCode": 0})
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	dockerService := (&docker.DockerClientService{}).WithClient(newVolumeWorkspaceTestDockerClient(t, server))
	service := NewVolumeService(nil, dockerService, nil, nil, nil, nil, nil, nil, nil, nil)
	service.helperByVolume["workspace-volume"] = &volumeHelper{id: "helper", lastUsedAt: time.Now(), protocol: acfstypes.ProtocolVersion}
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
			writeDockerExecAttachResponse(t, w, output)
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

	gormDB, err := gorm.Open(sqlite.Open("file:volume-workspace-stale-revision?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	settingsService, err := settings.NewSettingsService(t.Context(), &database.DB{DB: gormDB}, &config.Config{}, settings.Env{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context())))
	})

	dockerClient := newVolumeWorkspaceTestDockerClient(t, server)
	service := NewVolumeService(nil, (&docker.DockerClientService{}).WithClient(dockerClient), nil, settingsService, nil, nil, nil, nil, nil, nil)
	uploadIndex := 0
	workspace, err := service.workspace.UpdateVolumeWorkspace(t.Context(), "workspace-volume", types.VolumeWorkspaceUpdateManifest{
		FileTreeRevision: "stale",
		FileChanges: []types.VolumeWorkspaceFileChange{{
			Operation:    types.FileOpCreateFile,
			RelativePath: "new.txt",
			UploadIndex:  &uploadIndex,
		}},
	}, map[int][]byte{0: []byte("new content")}, types.Actor{})

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
		Destination:     types.VolumeBackupDestinationLocalS3,
		S3DestinationID: destination.ID,
	}).Error)

	service := NewVolumeService(db, nil, nil, nil, nil, nil, nil, s3.NewS3DestinationService(db, nil), nil, nil)
	backups, _, err := service.backup.List(t.Context(), "app-data", pagination.QueryParams{})
	require.NoError(t, err)
	require.Len(t, backups, 1)
	require.Equal(t, types.VolumeBackupDestinationLocalS3, backups[0].Destination)
	require.Equal(t, "Offsite", backups[0].S3DestinationName)
}

func TestVolumeBackupPolicy_GetReturnsLastRunForEachPolicyFromStore(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-last-runs-store?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	applyVolumeBackupMigrations(t, gormDB)
	require.NoError(t, gormDB.Create(&VolumeBackup{VolumeName: "app-data", PolicyID: "first-policy", Status: VolumeBackupStatusSucceeded}).Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{VolumeName: "app-data", PolicyID: "second-policy", Status: VolumeBackupStatusFailed}).Error)

	store := NewVolumeService(&database.DB{DB: gormDB}, nil, nil, nil, nil, nil, nil, nil, nil, nil).backupStore()
	first, err := store.Latest(t.Context(), "first-policy")
	require.NoError(t, err)
	require.Equal(t, "succeeded", first.Status)
	second, err := store.Latest(t.Context(), "second-policy")
	require.NoError(t, err)
	require.Equal(t, "failed", second.Status)
}

func volumeScopeContext(t *testing.T, target string, bodies map[string]any) (context.Context, *sync.Map) {
	t.Helper()
	var queries sync.Map
	return httpx.WithEnvironmentScope(t.Context(), &httpx.EnvironmentScope{
		IDs:     []string{"a", "b", "c"},
		Request: httptest.NewRequest(http.MethodGet, target, http.NoBody),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/environments/"), "/")[0]
			queries.Store(id, r.URL.Query())
			body, ok := bodies[id]
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"detail":"environment offline"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(body)
		}),
		Resolve: func(_ context.Context, id string) (types.EnvironmentRef, error) {
			return types.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	}), &queries
}

func TestAggregateVolumesMergesEnvironments(t *testing.T) {
	bodies := map[string]any{
		"a": types.PaginatedWithCounts[types.Volume, VolumeUsageCountsData]{Success: true, Data: []types.Volume{
			{Name: "data", Size: 30, InUse: true},
			{Name: "logs", Size: 5},
		}, Counts: VolumeUsageCountsData{Inuse: 1, Unused: 1, Total: 2}, Pagination: types.PaginationResponse{TotalItems: 2, GrandTotalItems: 3}},
		"b": types.PaginatedWithCounts[types.Volume, VolumeUsageCountsData]{Success: true, Data: []types.Volume{
			{Name: "cache", Size: 10},
		}, Counts: VolumeUsageCountsData{Unused: 1, Total: 1}, Pagination: types.PaginationResponse{TotalItems: 1}},
	}
	rowLabels := func(items []types.Volume) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item.EnvironmentID+"/"+item.Name)
		}
		return out
	}
	svc := &VolumeService{}

	t.Run("sorts by name across environments and pages", func(t *testing.T) {
		ctx, queries := volumeScopeContext(t, "/api/environments/a,b,c/volumes?start=0&limit=2&sort=name&order=asc", bodies)
		body, err := svc.AggregateVolumes(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "name", Order: pagination.SortAsc}, Params: pagination.Params{Limit: 2}})
		require.NoError(t, err)

		require.True(t, body.Success)
		require.Equal(t, []string{"b/cache", "a/data"}, rowLabels(body.Data))
		require.Equal(t, "Env b", body.Data[0].EnvironmentName)
		require.Equal(t, int64(3), body.Pagination.TotalItems)
		require.Equal(t, int64(4), body.Pagination.GrandTotalItems)
		require.Equal(t, int64(2), body.Pagination.TotalPages)
		require.Equal(t, VolumeUsageCountsData{Inuse: 1, Unused: 2, Total: 3}, body.Counts)
		require.Equal(t, []types.EnvironmentFailure{{Environment: types.EnvironmentRef{ID: "c", Name: "c"}, Message: "environment offline"}}, body.FailedEnvironments)
		for _, id := range []string{"a", "b"} {
			stored, ok := queries.Load(id)
			require.True(t, ok)
			require.Equal(t, "0", stored.(url.Values).Get("start"))
			require.Equal(t, "-1", stored.(url.Values).Get("limit"))
		}
	})

	t.Run("sorts by size reported by each environment", func(t *testing.T) {
		ctx, queries := volumeScopeContext(t, "/api/environments/a,b,c/volumes?sort=size&order=desc", bodies)
		body, err := svc.AggregateVolumes(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "size", Order: pagination.SortDesc}, Params: pagination.Params{Limit: -1}})
		require.NoError(t, err)
		require.Equal(t, []string{"a/data", "b/cache", "a/logs"}, rowLabels(body.Data))
		stored, _ := queries.Load("b")
		require.Equal(t, "size", stored.(url.Values).Get("sort"))
	})

	t.Run("sorts by environment", func(t *testing.T) {
		ctx, _ := volumeScopeContext(t, "/api/environments/a,b,c/volumes?sort=environment&order=desc", bodies)
		body, err := svc.AggregateVolumes(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "environment", Order: pagination.SortDesc}, Params: pagination.Params{Limit: -1}})
		require.NoError(t, err)
		require.Equal(t, []string{"b/cache", "a/data", "a/logs"}, rowLabels(body.Data))
	})

	t.Run("all environments failing returns empty data", func(t *testing.T) {
		ctx, _ := volumeScopeContext(t, "/api/environments/a,b,c/volumes", map[string]any{})
		body, err := svc.AggregateVolumes(ctx, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
		require.NoError(t, err)
		require.Empty(t, body.Data)
		require.Len(t, body.FailedEnvironments, 3)
	})
}

func TestAggregateVolumeSizesKeepsEnvironmentRows(t *testing.T) {
	ctx, _ := volumeScopeContext(t, "/api/environments/a,b,c/volumes/sizes", map[string]any{
		"a": types.ApiResponse[[]VolumeSizeInfo]{Success: true, Data: []VolumeSizeInfo{{Name: "data", Size: 30, RefCount: 1}, {Name: "logs", Size: 5}}},
		"b": types.ApiResponse[[]VolumeSizeInfo]{Success: true, Data: []VolumeSizeInfo{{Name: "data", Size: 7}}},
	})

	body, err := (&VolumeService{}).AggregateVolumeSizes(ctx)
	require.NoError(t, err)

	require.True(t, body.Success)
	require.Equal(t, []VolumeSizeInfo{
		{EnvironmentTag: types.EnvironmentTag{EnvironmentID: "a", EnvironmentName: "Env a"}, Name: "data", Size: 30, RefCount: 1},
		{EnvironmentTag: types.EnvironmentTag{EnvironmentID: "a", EnvironmentName: "Env a"}, Name: "logs", Size: 5},
		{EnvironmentTag: types.EnvironmentTag{EnvironmentID: "b", EnvironmentName: "Env b"}, Name: "data", Size: 7},
	}, body.Data)
	require.Len(t, body.FailedEnvironments, 1)
	require.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
}
