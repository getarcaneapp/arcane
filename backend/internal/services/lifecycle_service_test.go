package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
)

func setupLifecycleTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.SettingVariable{},
		&models.Project{},
		&models.GitOpsSync{},
		&models.Event{},
	))
	return &database.DB{DB: db}
}

func newLifecycleTestService(t *testing.T, db *database.DB) (*LifecycleService, *SettingsService) {
	t.Helper()
	settings, err := NewSettingsService(context.Background(), db)
	require.NoError(t, err)
	events := NewEventService(db, nil, nil)
	return NewLifecycleService(db, settings, events, nil), settings
}

func writeLifecycleProjectDirWithScript(t *testing.T, scriptRel, scriptBody string) string {
	t.Helper()
	dir := t.TempDir()
	if scriptRel != "" {
		full := filepath.Join(dir, scriptRel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(scriptBody), 0o644))
	}
	return dir
}

func TestParseLifecycleEnvText_Empty(t *testing.T) {
	got, err := parseLifecycleEnvTextInternal(nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	empty := "   "
	got, err = parseLifecycleEnvTextInternal(&empty)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseLifecycleEnvText_Valid(t *testing.T) {
	raw := "FOO=bar\nBAZ_2=qux"
	got, err := parseLifecycleEnvTextInternal(&raw)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"FOO": "bar", "BAZ_2": "qux"}, got)
}

func TestParseLifecycleEnvText_RejectsInvalidKeys(t *testing.T) {
	cases := map[string]string{
		"leading digit": "1FOO=bar",
		"dash":          "FOO-BAR=baz",
		"dot":           "foo.bar=baz",
		"no equals":     "FOO bar",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseLifecycleEnvTextInternal(&raw)
			require.Error(t, err)
		})
	}
}

func TestParseLifecycleExtraMountsText_Empty(t *testing.T) {
	got, err := parseLifecycleExtraMountsTextInternal(nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestParseLifecycleExtraMountsText_Valid(t *testing.T) {
	raw := "/host/age.key:/age.key:ro\n/host/data:/data"
	got, err := parseLifecycleExtraMountsTextInternal(&raw)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "/host/age.key", got[0].Source)
	assert.Equal(t, "/age.key", got[0].Target)
	assert.True(t, got[0].Readonly)
	assert.Equal(t, "/host/data", got[1].Source)
	assert.Equal(t, "/data", got[1].Target)
	assert.False(t, got[1].Readonly)
}

func TestParseLifecycleExtraMountsText_RejectsRelativePaths(t *testing.T) {
	cases := []string{
		"relative/path:/in/container",
		"/host/path:relative/target",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := parseLifecycleExtraMountsTextInternal(&raw)
			require.Error(t, err)
		})
	}
}

func TestParseLifecycleExtraMountsText_RejectsInvalidMode(t *testing.T) {
	raw := "/host/data:/data:wat"
	_, err := parseLifecycleExtraMountsTextInternal(&raw)
	require.Error(t, err)
}

func TestParseKeyValueEnv_HappyPath(t *testing.T) {
	stdout := "FOO=bar\n# comment line\n\nBAZ_2=hello world\n"
	got, err := parseKeyValueEnvInternal(stdout)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"FOO": "bar", "BAZ_2": "hello world"}, got)
}

func TestParseKeyValueEnv_RejectsMalformedLines(t *testing.T) {
	cases := map[string]string{
		"no equals":   "FOO bar",
		"empty key":   "=value",
		"bad key":     "foo-bar=baz",
		"digit start": "1FOO=bar",
	}
	for name, stdout := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseKeyValueEnvInternal(stdout)
			require.Error(t, err)
		})
	}
}

func TestParseKeyValueEnv_AllowsEqualsInValue(t *testing.T) {
	stdout := "TOKEN=abc=def=ghi"
	got, err := parseKeyValueEnvInternal(stdout)
	require.NoError(t, err)
	assert.Equal(t, "abc=def=ghi", got["TOKEN"])
}

func TestValidateScriptPath_HappyPath(t *testing.T) {
	dir := writeLifecycleProjectDirWithScript(t, "scripts/pre-deploy.sh", "#!/bin/sh\necho hi\n")
	err := validateScriptPathInternal(dir, "scripts/pre-deploy.sh")
	require.NoError(t, err)
}

func TestValidateScriptPath_RejectsAbsolute(t *testing.T) {
	dir := t.TempDir()
	err := validateScriptPathInternal(dir, "/etc/passwd")
	require.Error(t, err)
}

func TestValidateScriptPath_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	err := validateScriptPathInternal(dir, "../escape.sh")
	require.Error(t, err)
}

func TestValidateScriptPath_RejectsMissingFile(t *testing.T) {
	dir := t.TempDir()
	err := validateScriptPathInternal(dir, "missing.sh")
	require.Error(t, err)
}

func TestValidateScriptPath_RejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
	err := validateScriptPathInternal(dir, "scripts")
	require.Error(t, err)
}

func TestValidateScriptPath_RejectsSymlink(t *testing.T) {
	if _, err := os.Stat("/tmp"); err != nil {
		t.Skip("symlink test requires unix-like FS")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.sh")
	require.NoError(t, os.WriteFile(target, []byte("echo"), 0o644))
	link := filepath.Join(dir, "link.sh")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink in test env: %v", err)
	}
	err := validateScriptPathInternal(dir, "link.sh")
	require.Error(t, err)
}

func TestLifecycleStatusForResult(t *testing.T) {
	assert.Equal(t, lifecycleStatusSuccess, lifecycleStatusForResultInternal(0, nil))
	assert.Equal(t, lifecycleStatusFailed, lifecycleStatusForResultInternal(1, nil))
	assert.Equal(t, lifecycleStatusFailed, lifecycleStatusForResultInternal(0, errors.New("boom")))
	assert.Equal(t, lifecycleStatusTimeout, lifecycleStatusForResultInternal(0, context.DeadlineExceeded))
}

func TestCombineLifecycleOutput(t *testing.T) {
	cases := []struct {
		stdout, stderr, want string
	}{
		{"", "", ""},
		{"hello", "", "hello"},
		{"", "boom", "--- stderr ---\nboom"},
		{"hello\n", "boom\n", "hello\n--- stderr ---\nboom"},
	}
	for _, c := range cases {
		got := combineLifecycleOutputInternal(c.stdout, c.stderr)
		assert.Equal(t, c.want, got, "stdout=%q stderr=%q", c.stdout, c.stderr)
	}
}

func TestRunPreDeploy_NilProject(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc, _ := newLifecycleTestService(t, db)
	require.NoError(t, svc.RunPreDeploy(context.Background(), nil, models.User{}))
}

func TestRunPreDeploy_ProjectNotGitOpsManaged(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc, settings := newLifecycleTestService(t, db)
	require.NoError(t, settings.SetStringSetting(context.Background(), "lifecycleEnabled", "true"))

	project := &models.Project{
		Name:            "demo",
		Path:            t.TempDir(),
		GitOpsManagedBy: nil,
	}
	require.NoError(t, svc.RunPreDeploy(context.Background(), project, models.User{}))
}

func TestRunPreDeploy_KillSwitchDisabled(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc, _ := newLifecycleTestService(t, db)

	syncID := "sync-1"
	project := &models.Project{
		Name:            "demo",
		Path:            t.TempDir(),
		GitOpsManagedBy: &syncID,
	}
	project.ID = "proj-1"
	require.NoError(t, db.Create(project).Error)

	scriptPath := "pre-deploy.sh"
	runnerImage := "alpine:latest"
	sync := &models.GitOpsSync{
		Name:                 "demo-sync",
		EnvironmentID:        "0",
		RepositoryID:         "repo-1",
		Branch:               "main",
		ComposePath:          "docker-compose.yaml",
		TargetType:           "project",
		ProjectName:          "demo",
		ProjectID:            &project.ID,
		PreDeployScriptPath:  &scriptPath,
		PreDeployRunnerImage: &runnerImage,
	}
	sync.ID = syncID
	require.NoError(t, db.Create(sync).Error)

	require.NoError(t, svc.RunPreDeploy(context.Background(), project, models.User{}))
}

func TestRunPreDeploy_NoScriptConfigured(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc, settings := newLifecycleTestService(t, db)
	require.NoError(t, settings.SetStringSetting(context.Background(), "lifecycleEnabled", "true"))

	syncID := "sync-1"
	project := &models.Project{
		Name:            "demo",
		Path:            t.TempDir(),
		GitOpsManagedBy: &syncID,
	}
	project.ID = "proj-1"
	require.NoError(t, db.Create(project).Error)

	sync := &models.GitOpsSync{
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		Branch:        "main",
		ComposePath:   "docker-compose.yaml",
		TargetType:    "project",
		ProjectName:   "demo",
		ProjectID:     &project.ID,
	}
	sync.ID = syncID
	require.NoError(t, db.Create(sync).Error)

	require.NoError(t, svc.RunPreDeploy(context.Background(), project, models.User{}))
}

func TestRunPreDeploy_MissingRunnerImage(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc, settings := newLifecycleTestService(t, db)
	require.NoError(t, settings.SetStringSetting(context.Background(), "lifecycleEnabled", "true"))
	require.NoError(t, settings.SetStringSetting(context.Background(), "lifecycleDefaultRunnerImage", " "))

	syncID := "sync-1"
	projectDir := writeLifecycleProjectDirWithScript(t, "pre-deploy.sh", "echo hi\n")
	project := &models.Project{
		Name:            "demo",
		Path:            projectDir,
		GitOpsManagedBy: &syncID,
	}
	project.ID = "proj-1"
	require.NoError(t, db.Create(project).Error)

	scriptPath := "pre-deploy.sh"
	sync := &models.GitOpsSync{
		Name:                "demo-sync",
		EnvironmentID:       "0",
		RepositoryID:        "repo-1",
		Branch:              "main",
		ComposePath:         "docker-compose.yaml",
		TargetType:          "project",
		ProjectName:         "demo",
		ProjectID:           &project.ID,
		PreDeployScriptPath: &scriptPath,
	}
	sync.ID = syncID
	require.NoError(t, db.Create(sync).Error)

	err := svc.RunPreDeploy(context.Background(), project, models.User{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner image")
}

func TestResolveRunnerImage_UsesSettingDefault(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc, settings := newLifecycleTestService(t, db)
	require.NoError(t, settings.SetStringSetting(context.Background(), "lifecycleDefaultRunnerImage", "alpine:latest"))

	assert.Equal(t, "alpine:latest", svc.resolveRunnerImageInternal(context.Background(), &models.GitOpsSync{}))

	override := "debian:stable-slim"
	assert.Equal(t, override, svc.resolveRunnerImageInternal(context.Background(), &models.GitOpsSync{
		PreDeployRunnerImage: &override,
	}))
}

func TestRunPreDeploy_PathTraversalRejected(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc, settings := newLifecycleTestService(t, db)
	require.NoError(t, settings.SetStringSetting(context.Background(), "lifecycleEnabled", "true"))

	syncID := "sync-1"
	project := &models.Project{
		Name:            "demo",
		Path:            t.TempDir(),
		GitOpsManagedBy: &syncID,
	}
	project.ID = "proj-1"
	require.NoError(t, db.Create(project).Error)

	scriptPath := "../etc/passwd"
	runnerImage := "alpine:latest"
	sync := &models.GitOpsSync{
		Name:                 "demo-sync",
		EnvironmentID:        "0",
		RepositoryID:         "repo-1",
		Branch:               "main",
		ComposePath:          "docker-compose.yaml",
		TargetType:           "project",
		ProjectName:          "demo",
		ProjectID:            &project.ID,
		PreDeployScriptPath:  &scriptPath,
		PreDeployRunnerImage: &runnerImage,
	}
	sync.ID = syncID
	require.NoError(t, db.Create(sync).Error)

	err := svc.RunPreDeploy(context.Background(), project, models.User{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "script path")
}

func TestLoadGitOpsSyncForProject_ReturnsNilWhenAbsent(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc, _ := newLifecycleTestService(t, db)
	sync, err := svc.loadGitOpsSyncForProjectInternal(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, sync)
}

func TestPersistLastRun_UpdatesGitOpsSyncRow(t *testing.T) {
	db := setupLifecycleTestDB(t)
	svc, _ := newLifecycleTestService(t, db)

	sync := &models.GitOpsSync{
		Name:          "demo-sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		Branch:        "main",
		ComposePath:   "docker-compose.yaml",
		TargetType:    "project",
		ProjectName:   "demo",
	}
	sync.ID = "sync-1"
	require.NoError(t, db.Create(sync).Error)

	now := time.Now().UTC().Truncate(time.Second)
	svc.persistLastRunInternal(context.Background(), sync.ID, lifecycleStatusSuccess, "all good", now)

	var got models.GitOpsSync
	require.NoError(t, db.Where("id = ?", sync.ID).First(&got).Error)
	require.NotNil(t, got.PreDeployLastRunAt)
	require.NotNil(t, got.PreDeployLastRunStatus)
	require.NotNil(t, got.PreDeployLastRunOutput)
	assert.Equal(t, lifecycleStatusSuccess, *got.PreDeployLastRunStatus)
	assert.Equal(t, "all good", *got.PreDeployLastRunOutput)
}
