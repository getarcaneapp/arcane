package systembackup

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	recoverytypes "github.com/getarcaneapp/arcane/backend/v2/internal/recovery"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	sqlite "github.com/libtnb/sqlite"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"
)

func newSystemBackupAdmissionGateForTestInternal(t testing.TB) *actors.Gate[actors.AdmissionKey] {
	t.Helper()
	fxLifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), fxLifecycle)
	require.NoError(t, err)
	gate, err := actors.NewGate[actors.AdmissionKey](t.Context(), runtime, "system-backup-test-admission", t.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, gate.Stop(stopCtx))
		require.NoError(t, fxLifecycle.Stop(stopCtx))
	})
	return gate
}

type systemBackupPolicySchedulerInternal struct {
	jobs map[string]schedulertypes.Job
}

func (s *systemBackupPolicySchedulerInternal) AddJob(_ context.Context, job schedulertypes.Job) error {
	s.jobs[job.Name()] = job
	return nil
}

func (s *systemBackupPolicySchedulerInternal) RemoveJob(_ context.Context, name string) {
	delete(s.jobs, name)
}

func (s *systemBackupPolicySchedulerInternal) HasJob(name string) bool {
	_, ok := s.jobs[name]
	return ok
}

func TestRecoveryHelperExecutableInternal(t *testing.T) {
	path, executableMount, err := recoveryHelperExecutableInternal(nil, "/app/arcane")
	require.NoError(t, err)
	require.Equal(t, "/app/arcane", path)
	require.Nil(t, executableMount)

	path, executableMount, err = recoveryHelperExecutableInternal([]containertypes.MountPoint{{
		Type: mounttypes.TypeBind, Source: "/workspace/backend", Destination: "/app/backend", RW: true,
	}}, "/app/backend/.bin/arcane")
	require.NoError(t, err)
	require.Equal(t, systemRecoveryHelperPath, path)
	require.Equal(t, mounttypes.TypeBind, executableMount.Type)
	require.Equal(t, "/workspace/backend/.bin/arcane", executableMount.Source)
	require.Equal(t, systemRecoveryHelperPath, executableMount.Target)
	require.True(t, executableMount.ReadOnly)
}

func TestSystemBackupPoliciesRegisterIndependentJobs(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-backup-schedules?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupPolicy{}, &SystemBackupRun{}, &SystemBackupRecoveryConfig{}))
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "system-backup-policy-test-key-32bytes", Environment: "test"})
	service := &SystemBackupService{
		db:     &database.DB{DB: gormDB},
		config: &config.Config{DatabaseURL: "file:system-backup-schedules-test.db"},
		jobs:   entityjobs.New("system-backup:", backup.SystemAdmissionScope),
	}
	scheduler := &systemBackupPolicySchedulerInternal{jobs: make(map[string]schedulertypes.Job)}
	require.NoError(t, service.SetScheduler(context.Background(), scheduler, newSystemBackupAdmissionGateForTestInternal(t)))

	status, err := service.SetRecoveryKey(context.Background(), "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN")
	require.NoError(t, err)
	require.True(t, status.Configured)
	storedKey, err := service.recoveryKeyInternal(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN", storedKey)

	collection, err := service.UpdatePolicies(context.Background(), []backuptypes.UpdateSystemBackupPolicy{
		{Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 5, LocalEnabled: true},
		{Enabled: true, Schedule: "0 0 14 * * *", RetentionCount: 30, LocalEnabled: true},
	})
	require.NoError(t, err)
	require.True(t, collection.RecoveryKeyStored)
	require.Len(t, collection.Policies, 2)
	require.Len(t, scheduler.jobs, 2)

	firstID := collection.Policies[0].ID
	collection, err = service.UpdatePolicies(context.Background(), []backuptypes.UpdateSystemBackupPolicy{{
		ID: firstID, Enabled: true, Schedule: "0 */30 * * * *", RetentionCount: 9, LocalEnabled: true,
	}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	require.Equal(t, firstID, collection.Policies[0].ID)
	require.Equal(t, 9, collection.Policies[0].RetentionCount)
	require.Len(t, scheduler.jobs, 1)
}

func TestSystemBackupPolicyRequiresConfiguredRecoveryKeyWhenEnabled(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-backup-key-required?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupPolicy{}, &SystemBackupRecoveryConfig{}))
	service := &SystemBackupService{
		db:     &database.DB{DB: gormDB},
		config: &config.Config{DatabaseURL: "file:system-backup-key-required-test.db"},
	}

	_, err = service.UpdatePolicies(context.Background(), []backuptypes.UpdateSystemBackupPolicy{{
		Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 7, LocalEnabled: true,
	}})
	require.ErrorContains(t, err, "configure a recovery key")
}

func TestRecoveryEnvironmentInternalIncludesRuntimeSecrets(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:         "jwt-secret",
		EncryptionKey:     "encryption-secret",
		AdminStaticAPIKey: "admin-key",
		OidcClientSecret:  "oidc-secret",
		FilePerm:          0o640,
	}
	environment := (&SystemBackupService{config: cfg}).recoveryEnvironmentInternal(context.Background())
	require.Equal(t, "jwt-secret", environment["JWT_SECRET"])
	require.Equal(t, "encryption-secret", environment["ENCRYPTION_KEY"])
	require.Equal(t, "admin-key", environment["ADMIN_STATIC_API_KEY"])
	require.Equal(t, "oidc-secret", environment["OIDC_CLIENT_SECRET"])
	require.Equal(t, "0640", environment["FILE_PERM"])
}

func TestSystemSnapshotPathFromFilesInternal(t *testing.T) {
	path, err := systemSnapshotPathFromFilesInternal([]string{"/arcane.db", "/.arcane-recovery.json"})
	require.NoError(t, err)
	require.Equal(t, "/", path)

	path, err = systemSnapshotPathFromFilesInternal([]string{"/app/data/arcane.db", "/app/data/.arcane-recovery.json"})
	require.NoError(t, err)
	require.Equal(t, "/app/data", path)

	_, err = systemSnapshotPathFromFilesInternal([]string{"/arcane.db"})
	require.ErrorContains(t, err, "does not contain an Arcane recovery manifest")
}

func TestProjectFilesFromSnapshotInternal(t *testing.T) {
	tests := []struct {
		name         string
		files        []string
		snapshotPath string
		projectsPath string
		databasePath string
		expected     []string
	}{
		{
			name: "current snapshot root",
			files: []string{
				"/.arcane-recovery.json", "/arcane.db", "/arcane.db-wal", "/templates/demo.yaml",
				"/projects/demo/docker-compose.yaml", "/projects/demo/.env", "/projects/demo/data/", "/projects/demo/.env",
			},
			snapshotPath: "/", projectsPath: "projects", databasePath: "arcane.db",
			expected: []string{"demo/.env", "demo/docker-compose.yaml"},
		},
		{
			name: "legacy snapshot and custom projects root",
			files: []string{
				"/app/data/.arcane-recovery.json", "/app/data/arcane.db", "/app/data/custom/projects/nested/app/compose.yaml",
				"/app/data/projects/ignored/compose.yaml",
			},
			snapshotPath: "/app/data", projectsPath: "custom/projects", databasePath: "arcane.db",
			expected: []string{"nested/app/compose.yaml"},
		},
		{
			name: "projects directory is data root",
			files: []string{
				"/.arcane-recovery.json", "/.arcane-recovery-request.json", "/custom.db", "/custom.db-shm", "/demo/config.yaml",
			},
			snapshotPath: "/", projectsPath: "", databasePath: "custom.db",
			expected: []string{"demo/config.yaml"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := projectEntriesFromSnapshotInternal(test.files, test.snapshotPath, test.projectsPath, test.databasePath, "", true)
			require.Equal(t, test.expected, projectFilePathsInternal(entries))
		})
	}
}

func TestProjectsRelativePathFromManifestInternal(t *testing.T) {
	tests := []struct {
		name              string
		databaseURL       string
		projectsDirectory string
		expected          string
		errorContains     string
	}{
		{name: "absolute database path", databaseURL: "file:/app/data/arcane.db", projectsDirectory: "/app/data/historical", expected: "historical"},
		{name: "relative default database path", databaseURL: "file:data/arcane.db", projectsDirectory: "/app/data/historical", expected: "historical"},
		{name: "projects mapping", databaseURL: "file:/app/data/arcane.db", projectsDirectory: "/app/data/historical:/host/projects", expected: "historical"},
		{name: "data root", databaseURL: "file:/app/data/arcane.db", projectsDirectory: "/app/data", expected: ""},
		{name: "outside data", databaseURL: "file:/app/data/arcane.db", projectsDirectory: "/srv/projects", errorContains: "outside Arcane's system backup data"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := recoverytypes.Manifest{Environment: map[string]string{
				"DATABASE_URL":       test.databaseURL,
				"PROJECTS_DIRECTORY": test.projectsDirectory,
			}}
			relative, err := projectsRelativePathFromManifestInternal(manifest)
			if test.errorContains != "" {
				require.ErrorContains(t, err, test.errorContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, relative)
		})
	}
}

func TestInspectProjectBackupSnapshotManifestUsesDirectCurrentAndLegacyPathsInternal(t *testing.T) {
	manifest := recoveryManifestJSONForTestInternal(t, "file:/app/data/arcane.db", "/app/data/historical")
	for _, test := range []struct {
		name         string
		manifestPath string
		snapshotPath string
	}{
		{name: "current", manifestPath: "/.arcane-recovery.json", snapshotPath: "/"},
		{name: "legacy", manifestPath: "/app/data/.arcane-recovery.json", snapshotPath: "/app/data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var readPaths []string
			engine := &systemBackupSnapshotEngineStubInternal{
				listInternal: func(backup.Repository, string) ([]string, error) {
					require.FailNow(t, "manifest inspection must not recursively list the snapshot")
					return nil, nil
				},
				readInternal: func(_ backup.Repository, _ string, filePath string) (string, error) {
					readPaths = append(readPaths, filePath)
					if filePath == test.manifestPath {
						return manifest, nil
					}
					return "", errors.New("not found")
				},
				restoreInternal: func(backup.Repository, string, backup.RestoreOptions) error { return nil },
			}
			snapshot, databasePath, err := inspectProjectBackupSnapshotManifestInternal(
				t.Context(),
				engine,
				nil,
				systemBackupSnapshotLocationInternal{name: "test", snapshotID: "snapshot"},
				"key",
			)
			require.NoError(t, err)
			require.Equal(t, test.snapshotPath, snapshot.snapshotPath)
			require.Equal(t, "historical", snapshot.projectsPath)
			require.Equal(t, "arcane.db", databasePath)
			require.Contains(t, readPaths, test.manifestPath)
		})
	}
}

func TestProjectEntriesFromSnapshotSynthesizesFoldersAndExcludesProtectedDataInternal(t *testing.T) {
	entries := projectEntriesFromSnapshotInternal([]string{
		"/.arcane-recovery.json",
		"/.arcane-recovery-request.json",
		"/arcane.db",
		"/arcane.db-wal",
		"/arcane.db-shm",
		"/arcane.db-journal",
		"/demo/nested/compose.yaml",
		"/z.txt",
	}, "/", "", "arcane.db", "", false)
	require.Equal(t, []backuptypes.BackupFileEntry{
		{Path: "demo", Name: "demo", IsDirectory: true},
		{Path: "z.txt", Name: "z.txt"},
	}, entries)
}

func TestProjectEntriesFromSnapshotNestedBrowseInternal(t *testing.T) {
	entries := projectEntriesFromSnapshotInternal([]string{
		"/app/data/custom/projects/demo/nested/",
		"/app/data/custom/projects/demo/compose.yaml",
	}, "/app/data", "custom/projects", "arcane.db", "demo", false)
	require.Equal(t, []backuptypes.BackupFileEntry{
		{Path: "demo/nested", Name: "nested", IsDirectory: true},
		{Path: "demo/compose.yaml", Name: "compose.yaml"},
	}, entries)
}

func TestNormalizeSystemBackupSelectionInternal(t *testing.T) {
	snapshot := systemBackupSnapshotInternal{
		projectsPath: "historical",
		entries: []backuptypes.BackupFileEntry{
			{Path: "demo", Name: "demo", IsDirectory: true},
			{Path: "demo/compose.yaml", Name: "compose.yaml"},
		},
	}
	selected, err := normalizeSystemBackupSelectionInternal(backuptypes.RestoreSelection{SelectAll: true}, snapshot)
	require.NoError(t, err)
	require.Equal(t, []backuptypes.BackupFileEntry{{Path: "", Name: "historical", IsDirectory: true}}, selected)

	selected, err = normalizeSystemBackupSelectionInternal(
		backuptypes.RestoreSelection{Paths: []string{"demo/compose.yaml", "demo"}},
		snapshot,
	)
	require.NoError(t, err)
	require.Equal(t, []backuptypes.BackupFileEntry{{Path: "demo", Name: "demo", IsDirectory: true}}, selected)
}

type systemBackupSnapshotEngineStubInternal struct {
	listInternal    func(backup.Repository, string) ([]string, error)
	readInternal    func(backup.Repository, string, string) (string, error)
	restoreInternal func(backup.Repository, string, backup.RestoreOptions) error
}

func (s *systemBackupSnapshotEngineStubInternal) ListSnapshotFiles(_ context.Context, _ *client.Client, repository backup.Repository, _ string, snapshotID string) ([]string, error) {
	return s.listInternal(repository, snapshotID)
}

func (s *systemBackupSnapshotEngineStubInternal) ListSnapshotFilesAtPath(_ context.Context, _ *client.Client, repository backup.Repository, _ string, snapshotID, _ string, _ bool) ([]string, error) {
	return s.listInternal(repository, snapshotID)
}

func (s *systemBackupSnapshotEngineStubInternal) ReadSnapshotTextFile(_ context.Context, _ *client.Client, repository backup.Repository, _ string, snapshotID, filePath string) (string, error) {
	return s.readInternal(repository, snapshotID, filePath)
}

func (s *systemBackupSnapshotEngineStubInternal) RestoreSnapshot(_ context.Context, _ *client.Client, repository backup.Repository, _ string, snapshotID string, _ mounttypes.Mount, options backup.RestoreOptions) error {
	return s.restoreInternal(repository, snapshotID, options)
}

func recoveryManifestJSONForTestInternal(t testing.TB, databaseURL, projectsDirectory string) string {
	t.Helper()
	data, err := json.Marshal(recoverytypes.Manifest{
		FormatVersion: 1,
		Environment: map[string]string{
			"DATABASE_URL":       databaseURL,
			"PROJECTS_DIRECTORY": projectsDirectory,
		},
	})
	require.NoError(t, err)
	return string(data)
}

func TestAvailableProjectBackupSnapshotsFallsBackToRemoteInternal(t *testing.T) {
	manifest := recoveryManifestJSONForTestInternal(t, "file:/app/data/arcane.db", "/app/data/historical")
	tests := []struct {
		name      string
		locations []systemBackupSnapshotLocationInternal
		setupErr  error
	}{
		{
			name: "local snapshot unavailable",
			locations: []systemBackupSnapshotLocationInternal{
				{name: "local", repository: backup.Repository{ID: "local"}, snapshotID: "local-snapshot"},
				{name: "S3", repository: backup.Repository{ID: "remote"}, snapshotID: "remote-snapshot"},
			},
		},
		{
			name:      "local repository unavailable",
			locations: []systemBackupSnapshotLocationInternal{{name: "S3", repository: backup.Repository{ID: "remote"}, snapshotID: "remote-snapshot"}},
			setupErr:  errors.New("local repository is unavailable"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := &systemBackupSnapshotEngineStubInternal{
				listInternal: func(repository backup.Repository, _ string) ([]string, error) {
					if repository.ID == "local" {
						return nil, errors.New("local snapshot is unavailable")
					}
					return []string{"/.arcane-recovery.json", "/arcane.db", "/historical/demo/compose.yaml"}, nil
				},
				readInternal:    func(_ backup.Repository, _, _ string) (string, error) { return manifest, nil },
				restoreInternal: func(backup.Repository, string, backup.RestoreOptions) error { return nil },
			}

			snapshots, err := availableProjectBackupSnapshotsInternal(t.Context(), engine, nil, test.locations, "key", test.setupErr, true)
			require.NoError(t, err)
			require.Len(t, snapshots, 1)
			require.Equal(t, "remote-snapshot", snapshots[0].snapshotID)
			require.Equal(t, []string{"demo/compose.yaml"}, snapshots[0].files)
		})
	}
}

func TestSafetySnapshotFollowsRemoteRestoreSourceInternal(t *testing.T) {
	remoteRepository := backup.Repository{ID: "remote"}
	source := systemBackupSnapshotInternal{systemBackupSnapshotLocationInternal: systemBackupSnapshotLocationInternal{
		name: "S3", destination: backuptypes.SystemBackupDestinationS3, s3DestinationID: "destination",
		repository: remoteRepository, snapshotID: "source",
	}}
	request := safetyBackupRequestInternal(source, "key")
	require.Equal(t, backuptypes.SystemBackupDestinationS3, request.Destination)
	require.Equal(t, "destination", request.S3DestinationID)

	location, err := safetySnapshotLocationInternal(source, &SystemBackupRun{RemoteSnapshotID: "safety"})
	require.NoError(t, err)
	require.Equal(t, "safety", location.name)
	require.Equal(t, remoteRepository, location.repository)
	require.Equal(t, "safety", location.snapshotID)

	engine := &systemBackupSnapshotEngineStubInternal{
		listInternal: func(repository backup.Repository, snapshotID string) ([]string, error) {
			require.Equal(t, remoteRepository, repository)
			require.Equal(t, "safety", snapshotID)
			return []string{"/.arcane-recovery.json", "/projects/demo/compose.yaml"}, nil
		},
		readInternal:    func(backup.Repository, string, string) (string, error) { return "", nil },
		restoreInternal: func(backup.Repository, string, backup.RestoreOptions) error { return nil },
	}
	safety, err := prepareSafetySnapshotInternal(t.Context(), engine, nil, location, "key")
	require.NoError(t, err)
	require.Equal(t, remoteRepository, safety.repository)
	require.Contains(t, safety.dataPaths, "projects/demo/compose.yaml")
}

type systemBackupRestoreCallInternal struct {
	snapshotID string
	options    backup.RestoreOptions
}

func TestRestoreSelectedProjectFilesFallsBackToRemoteInternal(t *testing.T) {
	var calls []systemBackupRestoreCallInternal
	engine := &systemBackupSnapshotEngineStubInternal{
		listInternal: func(backup.Repository, string) ([]string, error) { return nil, nil },
		readInternal: func(backup.Repository, string, string) (string, error) { return "", nil },
		restoreInternal: func(_ backup.Repository, snapshotID string, options backup.RestoreOptions) error {
			calls = append(calls, systemBackupRestoreCallInternal{snapshotID: snapshotID, options: options})
			if snapshotID == "local-snapshot" {
				return errors.New("local restore failed")
			}
			return nil
		},
	}
	snapshots := []systemBackupSnapshotInternal{
		{systemBackupSnapshotLocationInternal: systemBackupSnapshotLocationInternal{name: "local", snapshotID: "local-snapshot"}, files: []string{"demo/compose.yaml"}, entries: []backuptypes.BackupFileEntry{{Path: "demo/compose.yaml", Name: "compose.yaml"}}, snapshotPath: "/", projectsPath: "projects"},
		{systemBackupSnapshotLocationInternal: systemBackupSnapshotLocationInternal{name: "S3", snapshotID: "remote-snapshot"}, files: []string{"demo/compose.yaml"}, entries: []backuptypes.BackupFileEntry{{Path: "demo/compose.yaml", Name: "compose.yaml"}}, snapshotPath: "/", projectsPath: "projects"},
	}
	removed := false
	err := restoreSelectedProjectFilesInternal(t.Context(), engine, nil, snapshots, systemBackupSafetySnapshotInternal{}, "key", []backuptypes.BackupFileEntry{{Path: "demo/compose.yaml", Name: "compose.yaml"}}, mounttypes.Mount{Target: "/arcane-data"}, func(string, string) error {
		removed = true
		return nil
	})
	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, []string{"local-snapshot", "remote-snapshot"}, []string{calls[0].snapshotID, calls[1].snapshotID})
}

func TestRestoreSelectedProjectFilesRollsBackHistoricalProjectRootInternal(t *testing.T) {
	var calls []systemBackupRestoreCallInternal
	engine := &systemBackupSnapshotEngineStubInternal{
		listInternal: func(backup.Repository, string) ([]string, error) { return nil, nil },
		readInternal: func(backup.Repository, string, string) (string, error) { return "", nil },
		restoreInternal: func(_ backup.Repository, snapshotID string, options backup.RestoreOptions) error {
			calls = append(calls, systemBackupRestoreCallInternal{snapshotID: snapshotID, options: options})
			if snapshotID == "source" && options.SourcePath == "/historical/demo/.env" {
				return errors.New("source restore failed")
			}
			return nil
		},
	}
	snapshots := []systemBackupSnapshotInternal{{
		systemBackupSnapshotLocationInternal: systemBackupSnapshotLocationInternal{name: "local", snapshotID: "source"},
		files:                                []string{"demo/compose.yaml", "demo/.env"},
		entries:                              []backuptypes.BackupFileEntry{{Path: "demo/compose.yaml", Name: "compose.yaml"}, {Path: "demo/.env", Name: ".env"}},
		snapshotPath:                         "/", projectsPath: "historical",
	}}
	safety := systemBackupSafetySnapshotInternal{
		systemBackupSnapshotLocationInternal: systemBackupSnapshotLocationInternal{name: "safety", snapshotID: "safety"},
		snapshotPath:                         "/",
		dataPaths:                            map[string]struct{}{"historical/demo/compose.yaml": {}, "current/demo/compose.yaml": {}},
	}
	var removed []string
	err := restoreSelectedProjectFilesInternal(t.Context(), engine, nil, snapshots, safety, "key", []backuptypes.BackupFileEntry{{Path: "demo/compose.yaml", Name: "compose.yaml"}, {Path: "demo/.env", Name: ".env"}}, mounttypes.Mount{Target: "/arcane-data"}, func(projectsPath, selectedPath string) error {
		removed = append(removed, projectsPath+"/"+selectedPath)
		return nil
	})
	require.ErrorContains(t, err, "affected project files were rolled back")
	require.Equal(t, []string{"source", "source", "safety"}, []string{calls[0].snapshotID, calls[1].snapshotID, calls[2].snapshotID})
	require.Equal(t, "/historical/demo/compose.yaml", calls[2].options.SourcePath)
	require.Equal(t, "/arcane-data/historical/demo/compose.yaml", calls[2].options.DestinationPath)
	require.Equal(t, []string{"historical/demo/.env"}, removed)
}

func TestRestoreSelectedProjectFoldersRollsBackWithDeleteInternal(t *testing.T) {
	var calls []systemBackupRestoreCallInternal
	engine := &systemBackupSnapshotEngineStubInternal{
		listInternal: func(backup.Repository, string) ([]string, error) { return nil, nil },
		readInternal: func(backup.Repository, string, string) (string, error) { return "", nil },
		restoreInternal: func(_ backup.Repository, snapshotID string, options backup.RestoreOptions) error {
			calls = append(calls, systemBackupRestoreCallInternal{snapshotID: snapshotID, options: options})
			if snapshotID == "source" && options.SourcePath == "/projects/new/" {
				return errors.New("source restore failed")
			}
			return nil
		},
	}
	selected := []backuptypes.BackupFileEntry{
		{Path: "existing", Name: "existing", IsDirectory: true},
		{Path: "new", Name: "new", IsDirectory: true},
	}
	snapshots := []systemBackupSnapshotInternal{{
		systemBackupSnapshotLocationInternal: systemBackupSnapshotLocationInternal{name: "local", snapshotID: "source"},
		entries:                              selected,
		snapshotPath:                         "/",
		projectsPath:                         "projects",
	}}
	safety := systemBackupSafetySnapshotInternal{
		systemBackupSnapshotLocationInternal: systemBackupSnapshotLocationInternal{name: "safety", snapshotID: "safety"},
		snapshotPath:                         "/",
		dataPaths:                            map[string]struct{}{"projects/existing/file.txt": {}},
	}
	var removed []string
	err := restoreSelectedProjectFilesInternal(
		t.Context(),
		engine,
		nil,
		snapshots,
		safety,
		"key",
		selected,
		mounttypes.Mount{Target: "/arcane-data"},
		func(projectsPath, selectedPath string) error {
			removed = append(removed, projectsPath+"/"+selectedPath)
			return nil
		},
	)
	require.ErrorContains(t, err, "affected project files were rolled back")
	require.Equal(t, []string{"projects/new"}, removed)
	require.True(t, calls[0].options.DeleteExtra)
	require.True(t, calls[1].options.DeleteExtra)
	require.Equal(t, "safety", calls[2].snapshotID)
	require.True(t, calls[2].options.DeleteExtra)
	require.Equal(t, "/projects/existing/", calls[2].options.SourcePath)
}
