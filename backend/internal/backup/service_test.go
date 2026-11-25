package backup

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	francistest "getarcane.dev/arcane/v3/pkg/francis/testing"
	"getarcane.dev/types/v3"
)

func TestMarkSnapshotDirectories(t *testing.T) {
	files := []string{"/volume", "/volume/folder", "/volume/file.txt", "/volume/link"}
	longOutput := "drwxr-xr-x root root 0 1 Jan 2026 00:00 \"/volume\"\r\ndrwxr-xr-x root root 0 1 Jan 2026 00:00 \"/volume/" +
		"folder\"\r\n-rw-r--r-- root root 5 1 Jan 2026 00:00 \"/volume/file.txt\"\r\nlrwxrwxrwx root root 4 1 Jan 20" +
		"26 00:00 \"/volume/link\" -> \"file.txt\""

	marked, err := markSnapshotDirectories(files, longOutput)
	require.NoError(t, err)
	require.Equal(t, []string{"/volume/", "/volume/folder/", "/volume/file.txt", "/volume/link"}, marked)
	require.Equal(t, []string{"/volume", "/volume/folder", "/volume/file.txt", "/volume/link"}, files)
}

func TestMarkSnapshotDirectoriesRejectsMismatchedListings(t *testing.T) {
	_, err := markSnapshotDirectories([]string{"/volume", "/volume/file.txt"}, "drwxr-xr-x root root 0 1 Jan 2026 00:00 \"/volume\"")
	require.ErrorContains(t, err, "different lengths")
}

func TestQualifySnapshotListing(t *testing.T) {
	tests := []struct {
		name         string
		files        []string
		snapshotPath string
		expected     []string
	}{
		{
			name:         "root listing",
			files:        []string{"folder/", "file.txt"},
			snapshotPath: "",
			expected:     []string{"folder/", "file.txt"},
		},
		{
			name:         "empty listing",
			files:        []string{},
			snapshotPath: "folder",
			expected:     []string{},
		},
		{
			name:         "nested listing",
			files:        []string{"nested/", "file.txt", "./link"},
			snapshotPath: "folder",
			expected:     []string{"folder/nested/", "folder/file.txt", "folder/link"},
		},
		{
			name:         "legacy system project listing",
			files:        []string{"demo/compose.yaml"},
			snapshotPath: "app/data/projects",
			expected:     []string{"app/data/projects/demo/compose.yaml"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qualified := qualifySnapshotListing(test.files, test.snapshotPath)
			require.Equal(t, test.expected, qualified)
			if len(test.files) > 0 {
				require.NotSame(t, &test.files[0], &qualified[0])
			}
		})
	}
}

func TestRecoveryKeyStoreRoundTrip(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:recovery-key-store?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupRecoveryConfig{}))
	crypto.InitEncryption(crypto.Config{EncryptionKey: "recovery-key-store-test-key-32bytes", Environment: "test"})
	store := NewRecoveryKeyStore(&database.DB{DB: gormDB})

	configured, err := store.Configured(t.Context())
	require.NoError(t, err)
	require.False(t, configured)

	_, err = store.Get(t.Context(), RecoveryKeyConfigID)
	require.ErrorIs(t, err, ErrRecoveryKeyNotConfigured)

	key, err := GenerateRecoveryKey()
	require.NoError(t, err)
	require.NoError(t, store.Set(t.Context(), RecoveryKeyConfigID, key))

	configured, err = store.Configured(t.Context())
	require.NoError(t, err)
	require.True(t, configured)

	stored, err := store.Get(t.Context(), RecoveryKeyConfigID)
	require.NoError(t, err)
	require.Equal(t, key, stored)
}

func TestRecoveryKeyValidation(t *testing.T) {
	require.NoError(t, ValidateRecoveryKey("QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN"))
	require.Error(t, ValidateRecoveryKey("too-short"))
	require.Error(t, ValidateRecoveryKey(""))
	require.Error(t, ValidateRecoveryKey("QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN-EXTRA"))

	key, err := GenerateRecoveryKey()
	require.NoError(t, err)
	require.NoError(t, ValidateRecoveryKey(key))
}

func TestEnvironmentRecoveryKey(t *testing.T) {
	const managerKey = "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN"
	first := EnvironmentRecoveryKey(managerKey, "env-1")
	require.NoError(t, ValidateRecoveryKey(first))
	require.Equal(t, first, EnvironmentRecoveryKey(" "+managerKey+" ", "env-1"))
	require.NotEqual(t, managerKey, first)
	require.NotEqual(t, first, EnvironmentRecoveryKey(managerKey, "env-2"))
	rotated, err := GenerateRecoveryKey()
	require.NoError(t, err)
	require.NotEqual(t, first, EnvironmentRecoveryKey(rotated, "env-1"))
}

func TestSnapshotCommand(t *testing.T) {
	single, err := snapshotCommand("volume", RootSnapshotInput(mount.Mount{Type: mount.TypeVolume, Source: "data", Target: "/volume"}))
	require.NoError(t, err)
	require.Equal(t, []string{"backup", "--init", "--json", "--host", "arcane", "--label", "volume", "--as-path", "/", "--", "/volume"}, single)

	multi, err := snapshotCommand("arcane-system-recovery", CreateSnapshotInput{Sources: []string{"/data", "/projects"}, Globs: []string{"!/data/arcane.db-wal"}})
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{
			"backup",
			"--init",
			"--json",
			"--host",
			"arcane",
			"--label",
			"arcane-system-recovery",
			"--glob",
			"!/data/arcane.db-wal",
			"--",
			"/data",
			"/projects",
		},
		multi,
	)

	_, err = snapshotCommand("x", CreateSnapshotInput{Sources: []string{"/data", "/projects"}, AsPath: "/"})
	require.ErrorContains(t, err, "single source")
	_, err = snapshotCommand("x", CreateSnapshotInput{})
	require.ErrorContains(t, err, "at least one snapshot source")
}

func TestExpiredUnmanagedRunIDs(t *testing.T) {
	db, err := database.Initialize(t.Context(), "file:backup-unmanaged-expiry?mode=memory&cache=shared", database.MigrationOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	now := time.Now()
	day := 24 * time.Hour
	insert := func(id, trigger, status string, createdAt time.Time, orphanedAt *time.Time) {
		require.NoError(t, db.Exec(
			"INSERT INTO volume_backups (id, volume_name, size, created_at, trigger, status, orphaned_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			id, "app-data", 0, createdAt, trigger, status, orphanedAt,
		).Error)
	}
	insert("safety-old", types.VolumeBackupTriggerSafety, types.VolumeBackupStatusSucceeded, now.Add(-8*day), nil)
	insert("safety-new", types.VolumeBackupTriggerSafety, types.VolumeBackupStatusSucceeded, now.Add(-day), nil)
	insert("safety-running", types.VolumeBackupTriggerSafety, types.VolumeBackupStatusRunning, now.Add(-30*day), nil)
	insert("orphan-new", types.VolumeBackupTriggerScheduled, types.VolumeBackupStatusSucceeded, now.Add(-30*day), nil)
	insert("orphan-stale", types.VolumeBackupTriggerScheduled, types.VolumeBackupStatusFailed, now.Add(-20*day), new(now.Add(-8*day)))
	insert("managed-again", types.VolumeBackupTriggerManual, types.VolumeBackupStatusSucceeded, now.Add(-10*day), new(now.Add(-8*day)))
	sweep := types.UnmanagedBackupSweep{Now: now, Retention: 7 * day}
	orphanedAt := func(id string) *time.Time {
		var row struct{ OrphanedAt *time.Time }
		require.NoError(t, db.Table("volume_backups").Select("orphaned_at").Where("id = ?", id).Scan(&row).Error)
		return row.OrphanedAt
	}

	expired, err := ExpiredUnmanagedRunIDs(t.Context(), db, "volume_backups", []string{"orphan-new", "orphan-stale"}, sweep)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"orphan-stale", "safety-old"}, expired)
	require.NotNil(t, orphanedAt("orphan-new"), "a newly orphaned backup starts its grace period instead of expiring by age")
	require.Nil(t, orphanedAt("managed-again"), "a backup managed again ends its grace period")

	recheck := types.UnmanagedBackupSweep{Now: now, Within: []string{"orphan-stale", "safety-new"}, Retention: 7 * day}
	expired, err = ExpiredUnmanagedRunIDs(t.Context(), db, "volume_backups", []string{"orphan-new", "orphan-stale"}, recheck)
	require.NoError(t, err)
	require.Equal(t, []string{"orphan-stale"}, expired, "a recheck only reports backups from the batch it locked")

	expired, err = ExpiredUnmanagedRunIDs(t.Context(), db, "volume_backups", nil, sweep)
	require.NoError(t, err)
	require.Equal(t, []string{"safety-old"}, expired)
	require.Nil(t, orphanedAt("orphan-stale"))
}

func TestCheckScheduledRemoteCountsEveryTableOfTheRepository(t *testing.T) {
	db, err := database.Initialize(t.Context(), "file:backup-scheduled-remote?mode=memory&cache=shared", database.MigrationOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
	}))
	t.Cleanup(storage.Close)
	crypto.InitEncryption(crypto.Config{EncryptionKey: "test-encryption-key-for-scheduled-remote-32", Environment: "test"})
	secret, err := crypto.Encrypt("secret-key")
	require.NoError(t, err)
	destination := &s3.S3Destination{
		Name: "Offsite", Endpoint: strings.TrimPrefix(storage.URL, "http://"), Bucket: "backups", Region: "us-east-1",
		AccessKeyID: "access-key", SecretAccessKey: secret, ForcePathStyle: true,
	}
	require.NoError(t, db.Create(destination).Error)
	destinations := s3.NewS3DestinationService(db, nil, func(context.Context, string) error { return nil })
	check := func(root string, tables []string) error {
		return CheckScheduledRemote(t.Context(), db, destinations, ScheduledRemoteRequest{DestinationID: destination.ID, Root: root, Tables: tables})
	}

	require.NoError(t, check(VolumeRoot+"/instance", VolumeRepositoryTables))
	require.NoError(t, db.Exec(
		"INSERT INTO project_backups (id, project_id, project_name, created_at, status, s3_destination_id, remote_snapshot_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"project-backup", "p1", "web", time.Now(), types.VolumeBackupStatusSucceeded, destination.ID, "remote",
	).Error)
	require.ErrorIs(t, check(VolumeRoot+"/instance", VolumeRepositoryTables), ErrRemoteRepositoryMissing)
	require.NoError(t, check("arcane-system-recovery", []string{"system_backup_runs"}))
}

func TestAwaitRunWaitsForConflictingRun(t *testing.T) {
	runtime := francistest.New(t)
	gate := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, gate.Register(runtime))
	francistest.Start(t, runtime)
	engine := NewEngine(gate, nil, nil)
	ctx := t.Context()

	held, admitted, err := engine.TryAcquireRun(ctx, SystemAdmissionScope, SystemAdmissionID)
	require.NoError(t, err)
	require.True(t, admitted)

	canceled, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	_, err = engine.AwaitRun(canceled, SystemAdmissionScope, SystemAdmissionID)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	acquired := make(chan *runs.Lease, 1)
	go func() {
		lease, awaitErr := engine.AwaitRun(ctx, SystemAdmissionScope, SystemAdmissionID)
		assert.NoError(t, awaitErr)
		acquired <- lease
	}()
	select {
	case <-acquired:
		t.Fatal("scheduled run was admitted while another run held the admission")
	case <-time.After(500 * time.Millisecond):
	}
	held.Release(ctx)
	select {
	case lease := <-acquired:
		require.NotNil(t, lease)
		lease.Release(ctx)
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled run was not admitted after the conflicting run finished")
	}
}

func TestStorageMountFromMounts(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		wantType     mount.Type
		wantSource   string
		wantTarget   string
		mounts       []container.MountPoint
		readOnly     bool
		wantResolved bool
		wantReadOnly bool
	}{
		{
			name: "mirrors bind mount",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups"},
			},
			target:       "/volume",
			readOnly:     true,
			wantResolved: true,
			wantType:     mount.TypeBind,
			wantSource:   "/host/backups",
			wantTarget:   "/volume",
			wantReadOnly: true,
		},
		{
			name: "writable request against read-only bind mount still resolves",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups", RW: false},
			},
			target:       "/volume",
			readOnly:     false,
			wantResolved: true,
			wantType:     mount.TypeBind,
			wantSource:   "/host/backups",
			wantTarget:   "/volume",
			wantReadOnly: false,
		},
		{
			name: "mirrors named volume",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "arcane-backups", Destination: "/backups"},
			},
			target:       "/backups",
			readOnly:     false,
			wantResolved: true,
			wantType:     mount.TypeVolume,
			wantSource:   "arcane-backups",
			wantTarget:   "/backups",
			wantReadOnly: false,
		},
		{
			name: "ignores unsupported mount types",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/backups"},
			},
			target:       "/backups",
			readOnly:     true,
			wantResolved: false,
		},
		{
			name:         "returns unresolved when mount is absent",
			target:       "/backups",
			readOnly:     true,
			wantResolved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storageMountFromMounts(t.Context(), tt.mounts, tt.target, tt.readOnly)
			require.Equal(t, tt.wantResolved, got != nil)
			if !tt.wantResolved {
				return
			}

			require.Equal(t, tt.wantType, got.Type)
			require.Equal(t, tt.wantSource, got.Source)
			require.Equal(t, tt.wantTarget, got.Target)
			require.Equal(t, tt.wantReadOnly, got.ReadOnly)
		})
	}
}

func setupContainersTest(t *testing.T, handler http.Handler) *Containers {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })
	return &Containers{
		Docker: newTestDockerService(t, dockerClient),
		Stop: func(ctx context.Context, containerID string, _ types.Actor) error {
			_, stopErr := dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{})
			return stopErr
		},
		Start: func(ctx context.Context, containerID string, _ types.Actor) error {
			_, startErr := dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
			return startErr
		},
	}
}

func TestContainersStopsAndRestartsOnlyRunningContainersUsingVolume(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			assert.NoError(t, json.MarshalWrite(w, []container.Summary{
				{ID: "uses-volume", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
				{ID: "other-volume", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "other-data"}}},
				{ID: "arcane", Labels: map[string]string{"com.getarcaneapp.arcane": "true"}, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/uses-volume/stop"):
			mu.Lock()
			operations = append(operations, "stop:uses-volume")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/uses-volume/start"):
			mu.Lock()
			operations = append(operations, "start:uses-volume")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	containers := setupContainersTest(t, serverHandler)
	actor := types.Actor{ID: "user-1", Username: "tester"}
	stopped, err := containers.StopContainers(t.Context(), ContainerScope{Volumes: []string{"app-data"}}, actor)
	require.NoError(t, err)
	require.Len(t, stopped, 1)
	require.Equal(t, "uses-volume", stopped[0].ID)

	remaining, err := containers.StartContainers(t.Context(), stopped, actor)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Equal(t, []string{"stop:uses-volume", "start:uses-volume"}, operations)
}

func TestContainersRollsBackStoppedContainersOnStopFailure(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			assert.NoError(t, json.MarshalWrite(w, []container.Summary{
				{ID: "first", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
				{ID: "second", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/first/stop"):
			mu.Lock()
			operations = append(operations, "stop:first")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/second/stop"):
			mu.Lock()
			operations = append(operations, "stop:second")
			mu.Unlock()
			http.Error(w, "stop failed", http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/containers/first/start"):
			mu.Lock()
			operations = append(operations, "start:first")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	containers := setupContainersTest(t, serverHandler)
	actor := types.Actor{ID: "user-1", Username: "tester"}
	stillStopped, err := containers.StopContainers(t.Context(), ContainerScope{Volumes: []string{"app-data"}}, actor)
	require.ErrorContains(t, err, "failed to stop container second")
	require.Empty(t, stillStopped)
	require.Equal(t, []string{"stop:first", "stop:second", "start:first"}, operations)
}

func TestContainersWaitsForRunningComposeReplacement(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	listCalls := 0
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			mu.Lock()
			listCalls++
			call := listCalls
			mu.Unlock()
			if call == 1 {
				assert.NoError(t, json.MarshalWrite(w, []container.Summary{
					{
						ID:     "old-id",
						Names:  []string{"/old-name"},
						State:  container.StateRunning,
						Labels: map[string]string{"com.docker.compose.project": "vault", "com.docker.compose.service": "vaultwarden", "com.docker.compose.container-number": "1"},
						Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}},
					},
				}))
				return
			}
			if call == 2 {
				assert.NoError(t, json.MarshalWrite(w, []container.Summary{}))
				return
			}
			assert.NoError(t, json.MarshalWrite(w, []container.Summary{
				{
					ID:     "new-id",
					Names:  []string{"/new-name"},
					State:  container.StateRunning,
					Labels: map[string]string{"com.docker.compose.project": "vault", "com.docker.compose.service": "vaultwarden", "com.docker.compose.container-number": "1"},
					Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}},
				},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/old-id/stop"):
			mu.Lock()
			operations = append(operations, "stop:old-id")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/start"):
			mu.Lock()
			operations = append(operations, "unexpected:"+r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	containers := setupContainersTest(t, serverHandler)
	actor := types.Actor{ID: "user-1", Username: "tester"}
	stopped, err := containers.StopContainers(t.Context(), ContainerScope{Volumes: []string{"app-data"}}, actor)
	require.NoError(t, err)
	require.Len(t, stopped, 1)
	require.Equal(t, "old-id", stopped[0].ID)

	remaining, err := containers.StartContainers(t.Context(), stopped, actor)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Equal(t, []string{"stop:old-id"}, operations)
}

func newTestDockerService(t *testing.T, dockerClient *client.Client) *docker.DockerClientService {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     db,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return docker.NewDockerClientService(t.Context(), db, settingsService, docker.ClientServiceOptions{}).WithClient(dockerClient)
}

func TestVolumeRepositoryPasswordPrefersRecoveryKey(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-password?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupRecoveryConfig{}))
	crypto.InitEncryption(crypto.Config{EncryptionKey: "volume-backup-password-test-key-32bytes", Environment: "test"})
	recoveryKeys := NewRecoveryKeyStore(&database.DB{DB: gormDB})
	repository := NewVolumeRepository(VolumeRepositoryDependencies{EncryptionKey: "legacy-instance-secret", RecoveryKeys: recoveryKeys})

	password, err := repository.Password(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, kit.SHA256Hex(legacyVolumePasswordSalt+"legacy-instance-secret"), password)

	recoveryKey := "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN"
	require.NoError(t, recoveryKeys.Set(t.Context(), RecoveryKeyConfigID, recoveryKey))
	password, err = repository.Password(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, recoveryKey, password)
}

func TestSourceMounts(t *testing.T) {
	mounts := []container.MountPoint{
		{Type: mount.TypeVolume, Name: "arcane-data", Destination: "/app/data", RW: true},
		{Type: mount.TypeBind, Source: "/host/projects", Destination: "/app/data/projects", RW: true},
		{Type: mount.TypeBind, Source: "/host/external", Destination: "/srv/projects", RW: true},
	}
	data, err := SourceMounts(SourceMountOptions{Mounts: mounts, ContainerPath: "/app/data", Target: "/data", InContainer: true})
	require.NoError(t, err)
	require.Equal(t, []mount.Mount{
		{Type: mount.TypeVolume, Source: "arcane-data", Target: "/data", ReadOnly: true},
		{Type: mount.TypeBind, Source: "/host/projects", Target: "/data/projects", ReadOnly: true},
	}, data)

	external, err := SourceMounts(SourceMountOptions{Mounts: mounts, ContainerPath: "/srv/projects", Target: "/projects", InContainer: true})
	require.NoError(t, err)
	require.Equal(t, []mount.Mount{{Type: mount.TypeBind, Source: "/host/external", Target: "/projects", ReadOnly: true}}, external)

	_, err = SourceMounts(SourceMountOptions{Mounts: mounts, ContainerPath: "/opt/projects", Target: "/projects", InContainer: true})
	require.ErrorContains(t, err, "/opt/projects must be mounted into the Arcane container")

	host, err := SourceMounts(SourceMountOptions{ContainerPath: "/tmp/data", Target: "/data"})
	require.NoError(t, err)
	require.Equal(t, []mount.Mount{{Type: mount.TypeBind, Source: "/tmp/data", Target: "/data", ReadOnly: true}}, host)
}

func TestRecordDiscoveredSkipsRecordedBackups(t *testing.T) {
	var created []string
	count, err := RecordDiscovered(t.Context(), DiscoveredRecords[*types.VolumeBackup]{
		Entries: []*types.VolumeBackup{{ID: "running"}, {ID: "new"}, {ID: "new"}},
		ID: func(entry *types.VolumeBackup) string {
			return entry.ID
		},
		Existing: func(_ context.Context, ids []string) ([]*types.VolumeBackup, error) {
			require.ElementsMatch(t, []string{"running", "new", "new"}, ids)
			return []*types.VolumeBackup{{ID: "running"}}, nil
		},
		Create: func(_ context.Context, entry *types.VolumeBackup) error {
			created = append(created, entry.ID)
			return nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, []string{"new"}, created)
}
