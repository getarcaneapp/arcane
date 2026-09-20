package systembackup

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	recoverytypes "github.com/getarcaneapp/arcane/types/v2/recovery"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/libtnb/sqlite"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
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
	submitted []schedulertypes.Request
	jobs      map[string]schedulertypes.Job
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
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupPolicy{}, &SystemBackupRun{}, &backup.SystemBackupRecoveryConfig{}))
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "system-backup-policy-test-key-32bytes", Environment: "test"})
	service := &SystemBackupService{
		db:           &database.DB{DB: gormDB},
		config:       &config.Config{DatabaseURL: "file:system-backup-schedules-test.db"},
		recoveryKeys: backup.NewRecoveryKeyStore(&database.DB{DB: gormDB}),
		jobs:         entityjobs.New("system-backup:", backup.SystemAdmissionScope),
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<Error><Code>NoSuchBucket</Code></Error>"))
	}))
	defer server.Close()
	require.NoError(t, gormDB.AutoMigrate(&s3domain.S3Destination{}))
	service.s3Destinations = s3domain.NewS3DestinationService(service.db)
	destination, err := service.s3Destinations.CreateS3Destination(t.Context(), backuptypes.CreateS3Destination{
		Name: "Missing storage", Endpoint: server.URL, Bucket: "backups", AccessKeyID: "test", SecretAccessKey: "test", ForcePathStyle: true,
	})
	require.NoError(t, err)
	for _, local := range []bool{false, true} {
		policy := &SystemBackupPolicy{Enabled: true, LocalEnabled: local, S3Enabled: true, S3DestinationID: destination.ID, Schedule: "0 0 2 * * *"}
		require.NoError(t, gormDB.Create(policy).Error)
		require.NoError(t, gormDB.Model(policy).Update("local_enabled", local).Error)
		service.rescheduleSystemBackupPolicyInternal(t.Context(), policy)
		disabled, err := service.disableMissingS3Internal(t.Context(), policy)
		require.NoError(t, err)
		require.True(t, disabled)
		require.NoError(t, gormDB.First(policy, "id = ?", policy.ID).Error)
		require.Equal(t, local, policy.Enabled)
		require.Equal(t, !local, policy.S3Enabled)
		require.Equal(t, local, scheduler.HasJob(service.jobs.JobName(policy.ID)))
	}

}

func TestSystemBackupPolicyRequiresConfiguredRecoveryKeyWhenEnabled(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:system-backup-key-required?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupPolicy{}, &backup.SystemBackupRecoveryConfig{}))
	service := &SystemBackupService{
		db:           &database.DB{DB: gormDB},
		config:       &config.Config{DatabaseURL: "file:system-backup-key-required-test.db"},
		recoveryKeys: backup.NewRecoveryKeyStore(&database.DB{DB: gormDB}),
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

func TestProjectFilesFromSnapshotInternal(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		layout   snapshotLayoutInternal
		expected []string
	}{
		{
			name: "version 1 snapshot root",
			files: []string{
				"/.arcane-recovery.json", "/arcane.db", "/arcane.db-wal", "/templates/demo.yaml",
				"/projects/demo/docker-compose.yaml", "/projects/demo/.env", "/projects/demo/data/", "/projects/demo/.env",
			},
			layout:   snapshotLayoutInternal{dataPath: "/", projectsPath: "/projects", databaseName: "arcane.db"},
			expected: []string{"demo/.env", "demo/docker-compose.yaml"},
		},
		{
			name: "legacy snapshot and custom projects root",
			files: []string{
				"/app/data/.arcane-recovery.json", "/app/data/arcane.db", "/app/data/custom/projects/nested/app/compose.yaml",
				"/app/data/projects/ignored/compose.yaml",
			},
			layout:   snapshotLayoutInternal{dataPath: "/app/data", projectsPath: "/app/data/custom/projects", databaseName: "arcane.db"},
			expected: []string{"nested/app/compose.yaml"},
		},
		{
			name: "projects directory is data root",
			files: []string{
				"/data/.arcane-recovery.json", "/data/.arcane-recovery-request.json", "/data/custom.db", "/data/custom.db-shm", "/data/demo/config.yaml",
			},
			layout:   snapshotLayoutInternal{dataPath: "/data", projectsPath: "/data", databaseName: "custom.db"},
			expected: []string{"demo/config.yaml"},
		},
		{
			name: "external projects keep database-like names",
			files: []string{
				"/data/.arcane-recovery.json", "/data/arcane.db", "/projects/arcane.db", "/projects/demo/compose.yaml",
			},
			layout:   snapshotLayoutInternal{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"},
			expected: []string{"arcane.db", "demo/compose.yaml"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := projectEntriesFromSnapshotInternal(test.files, test.layout, "", true)
			actual := make([]string, 0, len(entries))
			for _, entry := range entries {
				if !entry.IsDirectory {
					actual = append(actual, entry.Path)
				}
			}
			slices.Sort(actual)
			require.Equal(t, test.expected, actual)
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
		{name: "outside data", databaseURL: "file:/app/data/arcane.db", projectsDirectory: "/srv/projects", errorContains: errProjectsOutsideDataInternal.Error()},
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
	}, snapshotLayoutInternal{dataPath: "/", projectsPath: "/", databaseName: "arcane.db"}, "", false)
	require.Equal(t, []backuptypes.BackupFileEntry{
		{Path: "demo", Name: "demo", IsDirectory: true},
		{Path: "z.txt", Name: "z.txt"},
	}, entries)
}

func TestProjectEntriesFromSnapshotNestedBrowseInternal(t *testing.T) {
	entries := projectEntriesFromSnapshotInternal([]string{
		"/app/data/custom/projects/demo/nested/",
		"/app/data/custom/projects/demo/compose.yaml",
	}, snapshotLayoutInternal{dataPath: "/app/data", projectsPath: "/app/data/custom/projects", databaseName: "arcane.db"}, "demo", false)
	require.Equal(t, []backuptypes.BackupFileEntry{
		{Path: "demo/nested", Name: "nested", IsDirectory: true},
		{Path: "demo/compose.yaml", Name: "compose.yaml"},
	}, entries)
}

func TestNormalizeSystemBackupSelectionInternal(t *testing.T) {
	snapshot := systemBackupSnapshotInternal{
		layout: snapshotLayoutInternal{dataPath: "/data", projectsPath: "/data/historical", databaseName: "arcane.db"},
		entries: []backuptypes.BackupFileEntry{
			{Path: "demo", Name: "demo", IsDirectory: true},
			{Path: "demo/compose.yaml", Name: "compose.yaml"},
		},
	}
	selected, err := normalizeSystemBackupSelectionInternal(backuptypes.RestoreSelection{SelectAll: true}, snapshot)
	require.NoError(t, err)
	require.Equal(t, []backuptypes.BackupFileEntry{{Path: "", Name: "historical", IsDirectory: true}}, selected)

	_, err = normalizeSystemBackupSelectionInternal(
		backuptypes.RestoreSelection{SelectAll: true, Paths: []string{"demo"}},
		snapshot,
	)
	require.ErrorContains(t, err, "cannot be combined")

	selected, err = normalizeSystemBackupSelectionInternal(
		backuptypes.RestoreSelection{Paths: []string{"demo/compose.yaml", "demo"}},
		snapshot,
	)
	require.NoError(t, err)
	require.Equal(t, []backuptypes.BackupFileEntry{{Path: "demo", Name: "demo", IsDirectory: true}}, selected)

	snapshot.layout.projectsPath = snapshot.layout.dataPath
	selected, err = normalizeSystemBackupSelectionInternal(backuptypes.RestoreSelection{SelectAll: true}, snapshot)
	require.NoError(t, err)
	require.Equal(t, []backuptypes.BackupFileEntry{{Path: "demo", Name: "demo", IsDirectory: true}}, selected)
}

func (s *systemBackupPolicySchedulerInternal) Submit(_ context.Context, request schedulertypes.Request) (schedulertypes.Run, error) {
	s.submitted = append(s.submitted, request)
	return schedulertypes.Run{ID: request.RunID, JobID: request.JobID, EnvironmentID: request.EnvironmentID, Status: schedulertypes.Queued}, nil
}

func TestSnapshotLayoutFromManifestInternal(t *testing.T) {
	legacy := map[string]string{"DATABASE_URL": "file:/app/data/arcane.db", "PROJECTS_DIRECTORY": "/app/data/projects"}
	tests := []struct {
		name          string
		manifest      recoverytypes.Manifest
		root          string
		expected      snapshotLayoutInternal
		errorContains string
	}{
		{
			name: "version 1 projects inside data", manifest: recoverytypes.Manifest{FormatVersion: 1, Environment: legacy}, root: "/",
			expected: snapshotLayoutInternal{dataPath: "/", projectsPath: "/projects", databaseName: "arcane.db"},
		},
		{
			name:     "version 1 projects outside data are omitted",
			manifest: recoverytypes.Manifest{FormatVersion: 1, Environment: map[string]string{"DATABASE_URL": "file:/app/data/arcane.db", "PROJECTS_DIRECTORY": "/srv/projects"}},
			root:     "/app/data", expected: snapshotLayoutInternal{dataPath: "/app/data", databaseName: "arcane.db"},
		},
		{
			name: "version 2 external projects", manifest: recoverytypes.Manifest{FormatVersion: 2, DataPath: "/data", ProjectsPath: "/projects", DatabasePath: "arcane.db"}, root: "/data",
			expected: snapshotLayoutInternal{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"},
		},
		{
			name: "version 2 mismatched root", manifest: recoverytypes.Manifest{FormatVersion: 2, DataPath: "/data", ProjectsPath: "/projects", DatabasePath: "arcane.db"}, root: "/",
			errorContains: "records data path /data but was found at /",
		},
		{
			name: "version 2 traversal", manifest: recoverytypes.Manifest{FormatVersion: 2, DataPath: "/data", ProjectsPath: "/../etc", DatabasePath: "arcane.db"}, root: "/data",
			errorContains: "invalid projects path",
		},
		{
			name: "version 2 malformed database path", manifest: recoverytypes.Manifest{FormatVersion: 2, DataPath: "/data", ProjectsPath: "/data", DatabasePath: ""}, root: "/data",
			errorContains: "invalid database path",
		},
		{name: "unsupported version", manifest: recoverytypes.Manifest{FormatVersion: 3}, root: "/data", errorContains: "unsupported format 3"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			layout, err := snapshotLayoutFromManifestInternal(test.manifest, test.root)
			if test.errorContains != "" {
				require.ErrorContains(t, err, test.errorContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, layout)
		})
	}
}

func TestSnapshotLayoutProtectsDataFilesInternal(t *testing.T) {
	overlapping := snapshotLayoutInternal{dataPath: "/data", projectsPath: "/data", databaseName: "arcane.db"}
	require.True(t, overlapping.protectedInternal("arcane.db-wal"))
	require.True(t, overlapping.protectedInternal(".arcane-recovery.json"))
	require.False(t, overlapping.protectedInternal("demo/arcane.db"))
	nested := snapshotLayoutInternal{dataPath: "/data", projectsPath: "/data/projects", databaseName: "arcane.db"}
	require.False(t, nested.protectedInternal("arcane.db"))
	external := snapshotLayoutInternal{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}
	require.False(t, external.protectedInternal(".arcane-recovery.json"))
	require.False(t, snapshotLayoutInternal{dataPath: "/data"}.projectsIncludedInternal())
}

func TestProjectsSnapshotPathInternal(t *testing.T) {
	tests := []struct {
		name, data, projects, expected string
		external                       bool
		errorContains                  string
	}{
		{name: "inside data", data: "/app/data", projects: "/app/data/projects", expected: "/data/projects"},
		{name: "nested deeper", data: "/app/data", projects: "/app/data/custom/projects/", expected: "/data/custom/projects"},
		{name: "equal to data", data: "/app/data", projects: "/app/data", expected: "/data"},
		{name: "external bind", data: "/app/data", projects: "/host/path/to/projects", expected: "/projects", external: true},
		{name: "sibling with shared prefix", data: "/app/data", projects: "/app/datasets", expected: "/projects", external: true},
		{name: "ancestor of data", data: "/app/data", projects: "/app", errorContains: "contains Arcane's data directory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projectsPath, external, err := projectsSnapshotPathInternal(test.data, test.projects)
			if test.errorContains != "" {
				require.ErrorContains(t, err, test.errorContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, projectsPath)
			require.Equal(t, test.external, external)
		})
	}
}

func TestProjectsDirectoryInternalUsesContainerPathOfMapping(t *testing.T) {
	service := &SystemBackupService{config: &config.Config{ProjectsDirectory: "/app/data/projects:/host/projects"}}
	require.Equal(t, "/app/data/projects", service.projectsDirectoryInternal(context.Background()))
	require.Equal(t, "/app/data/projects:/host/projects", service.projectsSettingInternal(context.Background()))
}

func TestSourceMountsInternal(t *testing.T) {
	mounts := []containertypes.MountPoint{
		{Type: mounttypes.TypeVolume, Name: "arcane-data", Destination: "/app/data", RW: true},
		{Type: mounttypes.TypeBind, Source: "/host/projects", Destination: "/app/data/projects", RW: true},
		{Type: mounttypes.TypeBind, Source: "/host/external", Destination: "/srv/projects", RW: true},
	}
	data, err := sourceMountsInternal(mounts, true, "/app/data", "/data")
	require.NoError(t, err)
	require.Equal(t, []mounttypes.Mount{
		{Type: mounttypes.TypeVolume, Source: "arcane-data", Target: "/data", ReadOnly: true},
		{Type: mounttypes.TypeBind, Source: "/host/projects", Target: "/data/projects", ReadOnly: true},
	}, data)

	external, err := sourceMountsInternal(mounts, true, "/srv/projects", "/projects")
	require.NoError(t, err)
	require.Equal(t, []mounttypes.Mount{{Type: mounttypes.TypeBind, Source: "/host/external", Target: "/projects", ReadOnly: true}}, external)

	_, err = sourceMountsInternal(mounts, true, "/opt/projects", "/projects")
	require.ErrorContains(t, err, "/opt/projects must be mounted into the Arcane container")

	host, err := sourceMountsInternal(nil, false, "/tmp/data", "/data")
	require.NoError(t, err)
	require.Equal(t, []mounttypes.Mount{{Type: mounttypes.TypeBind, Source: "/tmp/data", Target: "/data", ReadOnly: true}}, host)
}

func TestWriteManifestInternalRecordsVersionTwoLayout(t *testing.T) {
	dataDirectory := t.TempDir()
	service := &SystemBackupService{config: &config.Config{DatabaseURL: "file:" + filepath.Join(dataDirectory, "arcane.db"), ProjectsDirectory: "/srv/projects"}}
	layout := backupSourceLayoutInternal{dataDirectory: dataDirectory, projectsDirectory: "/srv/projects", databaseName: "arcane.db", projectsPath: "/projects"}
	require.NoError(t, service.writeManifestInternal(context.Background(), "backup-1", layout))
	data, err := os.ReadFile(filepath.Join(dataDirectory, systemRecoveryManifestName))
	require.NoError(t, err)
	var manifest recoverytypes.Manifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	require.Equal(t, recoverytypes.ManifestFormatVersion, manifest.FormatVersion)
	require.Equal(t, "/srv/projects", manifest.Environment["PROJECTS_DIRECTORY"])
	resolved, err := snapshotLayoutFromManifestInternal(manifest, "/data")
	require.NoError(t, err)
	require.Equal(t, snapshotLayoutInternal{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}, resolved)
}

func TestRestoreTargetInternal(t *testing.T) {
	mounts := []containertypes.MountPoint{
		{Type: mounttypes.TypeVolume, Name: "arcane-data", Destination: "/app/data", RW: true},
		{Type: mounttypes.TypeBind, Source: "/host/projects", Destination: "/app/data/projects", RW: true},
		{Type: mounttypes.TypeBind, Source: "/host/templates", Destination: "/app/data/templates", RW: true},
		{Type: mounttypes.TypeBind, Source: "/host/ro", Destination: "/mnt/ro", RW: false},
	}
	target, err := restoreTargetInternal(mounts, "/app/data/custom/projects", "/restore-projects", "")
	require.NoError(t, err)
	require.Equal(t, "/restore-projects/custom/projects", target.Path)
	require.Equal(t, []mounttypes.Mount{{Type: mounttypes.TypeVolume, Source: "arcane-data", Target: "/restore-projects"}}, target.Mounts)

	data, err := restoreTargetInternal(mounts, "/app/data", "/restore", "/app/data/projects")
	require.NoError(t, err)
	require.Equal(t, "/restore", data.Path)
	require.Equal(t, []mounttypes.Mount{
		{Type: mounttypes.TypeVolume, Source: "arcane-data", Target: "/restore"},
		{Type: mounttypes.TypeBind, Source: "/host/templates", Target: "/restore/templates"},
	}, data.Mounts)

	_, err = restoreTargetInternal(mounts, "/mnt/ro/projects", "/restore-projects", "")
	require.ErrorContains(t, err, "mounted read-only")
	_, err = restoreTargetInternal(mounts, "/opt/projects", "/restore-projects", "")
	require.ErrorContains(t, err, "must be mounted into the Arcane container")
}

func TestRestoreStagesInternal(t *testing.T) {
	mounts := []containertypes.MountPoint{
		{Type: mounttypes.TypeVolume, Name: "arcane-data", Destination: "/app/data", RW: true},
		{Type: mounttypes.TypeBind, Source: "/host/nested", Destination: "/app/data/projects", RW: true},
		{Type: mounttypes.TypeBind, Source: "/host/external", Destination: "/srv/projects", RW: true},
	}
	repository := recoverytypes.RestoreRepository{Environment: []string{"RUSTIC_REPOSITORY=/repository"}}
	dataVolume := mounttypes.Mount{Type: mounttypes.TypeVolume, Source: "arcane-data", Target: "/restore"}
	nested := mounttypes.Mount{Type: mounttypes.TypeBind, Source: "/host/nested", Target: "/restore/projects"}

	t.Run("projects covered by data restore", func(t *testing.T) {
		layout := snapshotLayoutInternal{dataPath: "/data", projectsPath: "/data/projects", databaseName: "arcane.db"}
		stages, err := restoreStagesInternal(mounts, "/app/data", "/app/data/projects", repository, "snap", layout)
		require.NoError(t, err)
		require.Equal(t, []recoverytypes.RestoreStage{{Repository: repository, SnapshotID: "snap", SourcePath: "/data", Target: recoverytypes.RestoreTarget{Mounts: []mounttypes.Mount{dataVolume, nested}, Path: "/restore"}}}, stages)
	})
	t.Run("external projects restore separately", func(t *testing.T) {
		layout := snapshotLayoutInternal{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}
		stages, err := restoreStagesInternal(mounts, "/app/data", "/srv/projects", repository, "snap", layout)
		require.NoError(t, err)
		require.Len(t, stages, 2)
		require.Equal(t, "/data", stages[0].SourcePath)
		require.Equal(t, []mounttypes.Mount{dataVolume, nested}, stages[0].Target.Mounts)
		require.Equal(t, "/projects", stages[1].SourcePath)
		require.Equal(t, recoverytypes.RestoreTarget{Mounts: []mounttypes.Mount{{Type: mounttypes.TypeBind, Source: "/host/external", Target: "/restore-projects"}}, Path: "/restore-projects"}, stages[1].Target)
	})
	t.Run("changed projects directory excludes the nested mount from the data stage", func(t *testing.T) {
		layout := snapshotLayoutInternal{dataPath: "/", projectsPath: "/projects", databaseName: "arcane.db"}
		stages, err := restoreStagesInternal(mounts, "/app/data", "/app/data/projects", repository, "snap", layout)
		require.NoError(t, err)
		require.Len(t, stages, 1)
		require.Equal(t, []mounttypes.Mount{dataVolume, nested}, stages[0].Target.Mounts)

		external := snapshotLayoutInternal{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}
		stages, err = restoreStagesInternal(mounts, "/app/data", "/app/data/projects", repository, "snap", external)
		require.NoError(t, err)
		require.Len(t, stages, 2)
		require.Equal(t, []mounttypes.Mount{dataVolume}, stages[0].Target.Mounts)
		require.Equal(t, recoverytypes.RestoreTarget{Mounts: []mounttypes.Mount{{Type: mounttypes.TypeBind, Source: "/host/nested", Target: "/restore-projects"}}, Path: "/restore-projects"}, stages[1].Target)

		stages, err = restoreStagesInternal(mounts, "/app/data", "/app/data/custom/projects", repository, "snap", external)
		require.NoError(t, err)
		require.Len(t, stages, 2)
		require.Equal(t, []mounttypes.Mount{dataVolume, nested}, stages[0].Target.Mounts)
		require.Equal(t, recoverytypes.RestoreTarget{Mounts: []mounttypes.Mount{{Type: mounttypes.TypeVolume, Source: "arcane-data", Target: "/restore-projects"}}, Path: "/restore-projects/custom/projects"}, stages[1].Target)
	})
	t.Run("version 1 backup without projects restores data only", func(t *testing.T) {
		layout := snapshotLayoutInternal{dataPath: "/app/data", databaseName: "arcane.db"}
		stages, err := restoreStagesInternal(mounts, "/app/data", "/srv/projects", repository, "snap", layout)
		require.NoError(t, err)
		require.Len(t, stages, 1)
		require.Equal(t, "/app/data", stages[0].SourcePath)
	})
	t.Run("projects at data root cannot move", func(t *testing.T) {
		layout := snapshotLayoutInternal{dataPath: "/data", projectsPath: "/data", databaseName: "arcane.db"}
		_, err := restoreStagesInternal(mounts, "/app/data", "/srv/projects", repository, "snap", layout)
		require.ErrorContains(t, err, "keeps projects in Arcane's data directory")
		stages, err := restoreStagesInternal(mounts, "/app/data", "/app/data", repository, "snap", layout)
		require.NoError(t, err)
		require.Len(t, stages, 1)
	})
	t.Run("unmounted destination fails", func(t *testing.T) {
		layout := snapshotLayoutInternal{dataPath: "/data", projectsPath: "/projects", databaseName: "arcane.db"}
		_, err := restoreStagesInternal(mounts, "/app/data", "/opt/projects", repository, "snap", layout)
		require.ErrorContains(t, err, "/opt/projects must be mounted")
	})
}

func TestSafetySnapshotContainsPathInternal(t *testing.T) {
	safety := systemBackupSafetySnapshotInternal{paths: map[string]struct{}{"demo": {}, "demo/compose.yaml": {}}}
	require.True(t, safetySnapshotContainsPathInternal(safety, ""))
	require.True(t, safetySnapshotContainsPathInternal(safety, "demo"))
	require.True(t, safetySnapshotContainsPathInternal(safety, "demo/compose.yaml"))
	require.False(t, safetySnapshotContainsPathInternal(safety, "other"))
	require.ErrorContains(t, removeProjectFileInternal(context.Background(), t.TempDir(), ""), "refusing to remove")
}

func TestLegacyLayoutOmittedProjectsIsActionable(t *testing.T) {
	require.True(t, errors.Is(errProjectsNotInBackupInternal, errProjectsNotInBackupInternal))
	require.Contains(t, errProjectsNotInBackupInternal.Error(), "create a new system backup")
}
