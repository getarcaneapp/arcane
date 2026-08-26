package systembackup

import (
	"context"
	"slices"
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
}
