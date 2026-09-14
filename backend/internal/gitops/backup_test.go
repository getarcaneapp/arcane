package gitops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	git "github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/go-git/go-billy/v5/osfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var installBackupTestTransportOnceInternal sync.Once

// installBackupTestTransportInternal serves bare repositories on disk over the
// "http" scheme so backups push without a network or the git binary.
func installBackupTestTransportInternal() {
	installBackupTestTransportOnceInternal.Do(func() {
		client.InstallProtocol("http", server.NewClient(server.NewFilesystemLoader(osfs.New("/"))))
	})
}

// backupTestEnvInternal is one wired-up backup fixture: a bare remote, a git
// repository row, a project directory and the sync service under test.
type backupTestEnvInternal struct {
	service     *GitOpsSyncService
	db          *database.DB
	scheduler   *gitOpsBackupTestSchedulerInternal
	projectsDir string
	projectPath string
	project     *projectpkg.Project
	repoURL     string
	remote      *git.Client
}

// gitOpsBackupTestSchedulerInternal runs a submitted job body inline so the
// save-debounce path reaches PerformSync the way the real scheduler does.
type gitOpsBackupTestSchedulerInternal struct {
	mu        sync.Mutex
	jobs      map[string]schedulertypes.Job
	submitted []schedulertypes.Request
}

func (s *gitOpsBackupTestSchedulerInternal) AddJob(_ context.Context, job schedulertypes.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs == nil {
		s.jobs = make(map[string]schedulertypes.Job)
	}
	s.jobs[job.Name()] = job
	return nil
}

func (s *gitOpsBackupTestSchedulerInternal) RemoveJob(_ context.Context, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, name)
}

func (s *gitOpsBackupTestSchedulerInternal) HasJob(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.jobs[name]
	return ok
}

func (s *gitOpsBackupTestSchedulerInternal) Submit(ctx context.Context, request schedulertypes.Request) (schedulertypes.Run, error) {
	s.mu.Lock()
	s.submitted = append(s.submitted, request)
	job := s.jobs[request.JobID]
	s.mu.Unlock()
	if job != nil {
		_, _ = job.Run(ctx)
	}
	return schedulertypes.Run{ID: request.RunID, JobID: request.JobID, EnvironmentID: request.EnvironmentID, Status: schedulertypes.Queued}, nil
}

func (s *gitOpsBackupTestSchedulerInternal) submitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.submitted)
}

func setupGitOpsBackupTestServiceInternal(t *testing.T) *backupTestEnvInternal {
	t.Helper()
	installBackupTestTransportInternal()

	ctx := context.Background()
	db := setupGitOpsProjectTestDBInternal(t)
	require.NoError(t, db.AutoMigrate(&projectpkg.GitOpsSync{}, &projectpkg.ProjectTag{}, &gitrepo.GitRepository{}, &environment.Environment{}))

	settingsService, err := newGitOpsSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsDir := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, config.Load(), nil)
	projectService := projectpkg.NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())
	repoService := gitrepo.NewGitRepositoryService(db, t.TempDir(), eventService, settingsService)

	service := NewGitOpsSyncService(db, repoService, projectService, nil, eventService, settingsService)
	scheduler := &gitOpsBackupTestSchedulerInternal{}
	require.NoError(t, service.SetScheduler(ctx, scheduler, newGitOpsAdmissionGateForTestInternal(t)))

	bare := filepath.Join(t.TempDir(), "backups.git")
	_, err = gogit.PlainInit(bare, true)
	require.NoError(t, err)
	repoURL := "http://localhost" + bare

	require.NoError(t, db.Create(&gitrepo.GitRepository{
		BaseModel: database.BaseModel{ID: "repo-backup"},
		Name:      "backup-remote",
		URL:       repoURL,
		AuthType:  "none",
		Enabled:   true,
	}).Error)

	projectPath := filepath.Join(projectsDir, "demo-project")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	writeBackupProjectFileInternal(t, projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.27-alpine\n")

	project := &projectpkg.Project{
		BaseModel: database.BaseModel{ID: "proj-backup"},
		Name:      "demo-project",
		DirName:   new("demo-project"),
		Path:      projectPath,
		Status:    projectpkg.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	return &backupTestEnvInternal{
		service:     service,
		db:          db,
		scheduler:   scheduler,
		projectsDir: projectsDir,
		projectPath: projectPath,
		project:     project,
		repoURL:     repoURL,
		remote:      git.NewClient(t.TempDir()),
	}
}

func writeBackupProjectFileInternal(t *testing.T, root, relative, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte(content), 0o644))
}

func (e *backupTestEnvInternal) createBackupInternal(t *testing.T, req gitops.CreateSyncRequest) *projectpkg.GitOpsSync {
	t.Helper()
	if req.Name == "" {
		req.Name = "demo-backup"
	}
	if req.RepositoryID == "" {
		req.RepositoryID = "repo-backup"
	}
	if req.Branch == "" {
		req.Branch = "main"
	}
	if req.Mode == "" {
		req.Mode = gitops.SyncModeBackup
	}
	if req.ProjectID == "" {
		req.ProjectID = e.project.ID
	}
	if req.BackupDirectory == "" {
		req.BackupDirectory = "backups/demo"
	}
	syncRecord, err := e.service.CreateSync(t.Context(), "0", req, common.SystemUser)
	require.NoError(t, err)
	return syncRecord
}

func (e *backupTestEnvInternal) reloadInternal(t *testing.T, id string) *projectpkg.GitOpsSync {
	t.Helper()
	var syncRecord projectpkg.GitOpsSync
	require.NoError(t, e.db.Where("id = ?", id).First(&syncRecord).Error)
	return &syncRecord
}

// checkoutRemoteInternal clones the backup branch so the pushed tree can be inspected.
func (e *backupTestEnvInternal) checkoutRemoteInternal(t *testing.T, branch string) string {
	t.Helper()
	repoPath, err := e.remote.Clone(t.Context(), e.repoURL, branch, git.AuthConfig{AuthType: "none"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.remote.Cleanup(repoPath) })
	return repoPath
}

func (e *backupTestEnvInternal) remoteHeadInternal(t *testing.T, branch string) string {
	t.Helper()
	head, exists, err := e.remote.RemoteBranchHead(t.Context(), e.repoURL, branch, git.AuthConfig{AuthType: "none"})
	require.NoError(t, err)
	require.True(t, exists)
	return head
}

// pushRemoteCommitInternal commits directly to the branch as another writer.
func (e *backupTestEnvInternal) pushRemoteCommitInternal(t *testing.T, branch, message string, files map[string]string) {
	t.Helper()
	auth := git.AuthConfig{AuthType: "none"}
	checkout, err := e.remote.CheckoutForWrite(t.Context(), e.repoURL, branch, auth)
	require.NoError(t, err)
	defer func() { _ = e.remote.Cleanup(checkout.RepoPath) }()

	request := git.CommitRequest{Message: message, AuthorName: "Other", AuthorEmail: "other@localhost"}
	for path, content := range files {
		request.Files = append(request.Files, git.CommitFile{Path: path, Content: []byte(content)})
	}
	_, _, err = e.remote.CommitAndPush(t.Context(), checkout, request, auth)
	require.NoError(t, err)
}

func readBackupManifestInternal(t *testing.T, repoPath, directory string) gitops.BackupManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoPath, filepath.FromSlash(directory), gitops.BackupManifestFileName))
	require.NoError(t, err)
	var manifest gitops.BackupManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	return manifest
}

func TestGitOpsBackup_FirstRunPushesFilesAndManifest(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	writeBackupProjectFileInternal(t, env.projectPath, "config/app.conf", "key = value\n")

	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{BackupPaths: []string{"compose.yaml", "config"}})

	require.Equal(t, gitops.SyncModeBackup, syncRecord.Mode)
	require.Equal(t, "backups/demo", syncRecord.BackupDirectory)
	require.Equal(t, "backups/demo/compose.yaml", syncRecord.ComposePath)

	stored := env.reloadInternal(t, syncRecord.ID)
	require.NotNil(t, stored.LastSyncStatus)
	assert.Equal(t, "success", *stored.LastSyncStatus)
	assert.NotNil(t, stored.LastBackupAt)
	assert.Equal(t, gitops.BackupStateBackedUp, stored.BackupState())
	assert.False(t, stored.BackupConflict)
	assert.Nil(t, stored.BackupFailureReason)
	require.NotNil(t, stored.LastSyncCommit)
	assert.Equal(t, env.remoteHeadInternal(t, "main"), *stored.LastSyncCommit)
	require.NotNil(t, stored.LastBackupSnapshot)

	repoPath := env.checkoutRemoteInternal(t, "main")
	composeBytes, err := os.ReadFile(filepath.Join(repoPath, "backups", "demo", "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.27-alpine")
	assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", "config", "app.conf"))

	manifest := readBackupManifestInternal(t, repoPath, "backups/demo")
	assert.Equal(t, syncRecord.ID, manifest.SyncID)
	assert.Equal(t, "demo-project", manifest.ProjectName)
	assert.Contains(t, manifest.ComposeFiles, "compose.yaml")
	assert.Len(t, manifest.Files, 2)
	assert.Equal(t, hashBackupContentInternal(composeBytes), manifest.Files["compose.yaml"])
}

func TestGitOpsBackup_UnchangedContentMakesNoNewCommit(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{})
	firstHead := env.remoteHeadInternal(t, "main")

	result, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, common.SystemUser)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "already contains")
	assert.Equal(t, firstHead, env.remoteHeadInternal(t, "main"))

	stored := env.reloadInternal(t, syncRecord.ID)
	require.NotNil(t, stored.LastSyncStatus)
	assert.Equal(t, "success", *stored.LastSyncStatus)
	assert.Equal(t, gitops.BackupStateBackedUp, stored.BackupState())
}

func TestGitOpsBackup_AddsAndRemovesFilesAndKeepsUnrelatedRemoteFiles(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	writeBackupProjectFileInternal(t, env.projectPath, "config/app.conf", "key = value\n")
	writeBackupProjectFileInternal(t, env.projectPath, "config/extra.conf", "extra = 1\n")
	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{BackupPaths: []string{"compose.yaml", "config"}})

	env.pushRemoteCommitInternal(t, "main", "unrelated docs", map[string]string{"docs/readme.md": "docs\n"})

	require.NoError(t, os.Remove(filepath.Join(env.projectPath, "config", "extra.conf")))
	writeBackupProjectFileInternal(t, env.projectPath, "scripts/run.sh", "#!/bin/sh\necho hi\n")

	_, err := env.service.UpdateSync(t.Context(), "0", syncRecord.ID, gitops.UpdateSyncRequest{
		BackupPaths: []string{"compose.yaml", "config", "scripts"},
	}, common.SystemUser)
	require.NoError(t, err)

	result, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, common.SystemUser)
	require.NoError(t, err)
	require.True(t, result.Success)

	repoPath := env.checkoutRemoteInternal(t, "main")
	assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", "scripts", "run.sh"))
	assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", "config", "app.conf"))
	assert.FileExists(t, filepath.Join(repoPath, "docs", "readme.md"))
	assert.NoFileExists(t, filepath.Join(repoPath, "backups", "demo", "config", "extra.conf"))

	manifest := readBackupManifestInternal(t, repoPath, "backups/demo")
	assert.NotContains(t, manifest.Files, "config/extra.conf")
	assert.Contains(t, manifest.Files, "scripts/run.sh")
}

func TestGitOpsBackup_EnvFilesOnlyIncludedWhenListedExplicitly(t *testing.T) {
	tests := []struct {
		name        string
		paths       []string
		wantEnvFile bool
	}{
		{name: "directory selection skips env files", paths: []string{"compose.yaml", "config"}, wantEnvFile: false},
		{name: "explicit env file is included", paths: []string{"compose.yaml", "config", ".env"}, wantEnvFile: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := setupGitOpsBackupTestServiceInternal(t)
			writeBackupProjectFileInternal(t, env.projectPath, ".env", "TOKEN=secret\n")
			writeBackupProjectFileInternal(t, env.projectPath, "config/app.conf", "key = value\n")
			writeBackupProjectFileInternal(t, env.projectPath, "config/.env", "TOKEN=nested\n")
			writeBackupProjectFileInternal(t, env.projectPath, "config/staging.env", "TOKEN=staging\n")

			env.createBackupInternal(t, gitops.CreateSyncRequest{BackupPaths: test.paths})

			repoPath := env.checkoutRemoteInternal(t, "main")
			manifest := readBackupManifestInternal(t, repoPath, "backups/demo")
			assert.Contains(t, manifest.Files, "compose.yaml")
			assert.Contains(t, manifest.Files, "config/app.conf")
			assert.NotContains(t, manifest.Files, "config/.env")
			assert.NotContains(t, manifest.Files, "config/staging.env")
			if test.wantEnvFile {
				assert.Contains(t, manifest.Files, ".env")
				assert.FileExists(t, filepath.Join(repoPath, "backups", "demo", ".env"))
			} else {
				assert.NotContains(t, manifest.Files, ".env")
				assert.NoFileExists(t, filepath.Join(repoPath, "backups", "demo", ".env"))
			}
		})
	}
}

func TestGitOpsBackup_RemoteEditConflictsThenResolvesWithArcane(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{})

	env.pushRemoteCommitInternal(t, "main", "edited backup outside arcane", map[string]string{
		"backups/demo/compose.yaml": "services:\n  app:\n    image: tampered\n",
	})
	writeBackupProjectFileInternal(t, env.projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.28-alpine\n")

	_, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, common.SystemUser)
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrConflict)

	stored := env.reloadInternal(t, syncRecord.ID)
	assert.True(t, stored.BackupConflict)
	assert.Equal(t, gitops.BackupStateNeedsAttention, stored.BackupState())
	require.NotNil(t, stored.BackupFailureReason)
	assert.Equal(t, gitops.BackupFailureConflict, *stored.BackupFailureReason)

	preview, err := env.service.PreviewBackup(t.Context(), "0", syncRecord.ID)
	require.NoError(t, err)
	assert.Equal(t, backupPreviewConflict, preview.State)
	assert.NotEmpty(t, preview.Conflicts)

	result, err := env.service.ResolveBackupConflict(t.Context(), "0", syncRecord.ID, gitops.ResolveBackupConflictRequest{
		Strategy: gitops.BackupConflictUseArcane,
	}, common.SystemUser)
	require.NoError(t, err)
	require.True(t, result.Success)

	resolved := env.reloadInternal(t, syncRecord.ID)
	assert.False(t, resolved.BackupConflict)
	assert.Nil(t, resolved.BackupFailureReason)
	assert.Equal(t, gitops.BackupStateBackedUp, resolved.BackupState())

	repoPath := env.checkoutRemoteInternal(t, "main")
	composeBytes, err := os.ReadFile(filepath.Join(repoPath, "backups", "demo", "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.28-alpine")
}

func TestGitOpsBackup_OccupiedDestinationNeedsAttention(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	env.pushRemoteCommitInternal(t, "main", "pre-existing files", map[string]string{
		"backups/demo/unrelated.txt": "not an arcane backup\n",
	})

	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{})

	stored := env.reloadInternal(t, syncRecord.ID)
	assert.True(t, stored.BackupConflict)
	assert.Equal(t, gitops.BackupStateNeedsAttention, stored.BackupState())
	require.NotNil(t, stored.BackupFailureReason)
	assert.Equal(t, gitops.BackupFailureDestinationOccupied, *stored.BackupFailureReason)
	assert.Nil(t, stored.LastBackupAt)
}

func TestGitOpsBackup_AdoptsMatchingRemoteWithoutSnapshot(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{})
	head := env.remoteHeadInternal(t, "main")

	require.NoError(t, env.db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).
		Update("last_backup_snapshot", nil).Error)

	result, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, common.SystemUser)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, head, env.remoteHeadInternal(t, "main"))

	stored := env.reloadInternal(t, syncRecord.ID)
	assert.False(t, stored.BackupConflict)
	assert.Equal(t, gitops.BackupStateBackedUp, stored.BackupState())
}

func TestGitOpsBackup_SucceedsOnTopOfAnotherWritersCommit(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{})

	env.pushRemoteCommitInternal(t, "main", "other writer", map[string]string{"other/notes.txt": "notes\n"})
	writeBackupProjectFileInternal(t, env.projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.29-alpine\n")

	result, err := env.service.PerformSync(t.Context(), "0", syncRecord.ID, common.SystemUser)
	require.NoError(t, err)
	require.True(t, result.Success)

	repoPath := env.checkoutRemoteInternal(t, "main")
	assert.FileExists(t, filepath.Join(repoPath, "other", "notes.txt"))
	composeBytes, err := os.ReadFile(filepath.Join(repoPath, "backups", "demo", "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.29-alpine")
}

func TestGitOpsBackup_CreateValidation(t *testing.T) {
	syncDirectory := true
	scriptPath := "deploy.sh"

	tests := []struct {
		name    string
		mutate  func(t *testing.T, env *backupTestEnvInternal)
		request gitops.CreateSyncRequest
		wantErr error
	}{
		{
			name: "project deployed from git",
			mutate: func(t *testing.T, env *backupTestEnvInternal) {
				t.Helper()
				require.NoError(t, env.db.Model(&projectpkg.Project{}).Where("id = ?", env.project.ID).
					Update("gitops_managed_by", "sync-deploy").Error)
			},
			wantErr: common.ErrConflict,
		},
		{
			name: "second backup for the same project",
			mutate: func(t *testing.T, env *backupTestEnvInternal) {
				t.Helper()
				env.createBackupInternal(t, gitops.CreateSyncRequest{})
			},
			request: gitops.CreateSyncRequest{Name: "second", BackupDirectory: "backups/other"},
			wantErr: common.ErrConflict,
		},
		{
			name: "overlapping destination on the same branch",
			mutate: func(t *testing.T, env *backupTestEnvInternal) {
				t.Helper()
				other := &projectpkg.Project{
					BaseModel: database.BaseModel{ID: "proj-other"},
					Name:      "other-project",
					DirName:   new("other-project"),
					Path:      filepath.Join(env.projectsDir, "other-project"),
					Status:    projectpkg.ProjectStatusStopped,
				}
				writeBackupProjectFileInternal(t, other.Path, "compose.yaml", "services: {}\n")
				require.NoError(t, env.db.Create(other).Error)
				env.createBackupInternal(t, gitops.CreateSyncRequest{})
				env.project = other
			},
			request: gitops.CreateSyncRequest{Name: "nested", BackupDirectory: "backups/demo/nested"},
			wantErr: common.ErrConflict,
		},
		{
			name:    "selection missing the compose file",
			request: gitops.CreateSyncRequest{BackupPaths: []string{"config"}},
			wantErr: common.ErrValidation,
		},
		{
			name:    "deployment-only directory sync",
			request: gitops.CreateSyncRequest{SyncDirectory: &syncDirectory},
			wantErr: common.ErrValidation,
		},
		{
			name:    "deployment-only pre-deploy script",
			request: gitops.CreateSyncRequest{PreDeployScriptPath: &scriptPath},
			wantErr: common.ErrValidation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := setupGitOpsBackupTestServiceInternal(t)
			writeBackupProjectFileInternal(t, env.projectPath, "config/app.conf", "key = value\n")
			if test.mutate != nil {
				test.mutate(t, env)
			}
			request := test.request
			request.Mode = gitops.SyncModeBackup
			if request.Name == "" {
				request.Name = "demo-backup"
			}
			request.RepositoryID = "repo-backup"
			request.Branch = "main"
			request.ProjectID = env.project.ID
			if request.BackupDirectory == "" {
				request.BackupDirectory = "backups/demo"
			}

			_, err := env.service.CreateSync(t.Context(), "0", request, common.SystemUser)
			require.Error(t, err)
			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestGitOpsBackup_CreateWithoutProjectIsRejected(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)

	_, err := env.service.CreateSync(t.Context(), "0", gitops.CreateSyncRequest{
		Name:            "demo-backup",
		RepositoryID:    "repo-backup",
		Branch:          "main",
		Mode:            gitops.SyncModeBackup,
		BackupDirectory: "backups/demo",
	}, common.SystemUser)

	require.Error(t, err)
	assert.ErrorIs(t, err, common.ErrValidation)
}

func TestGitOpsBackup_SaveSignalMarksPendingAndRunsAfterDebounce(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{})
	firstHead := env.remoteHeadInternal(t, "main")

	env.service.backups.debounce = 50 * time.Millisecond
	env.service.SubscribeProjectFileChanges(t.Context())

	writeBackupProjectFileInternal(t, env.projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.29-alpine\n")
	env.service.projectService.FilesChanged().Publish(env.project.ID)

	require.Eventually(t, func() bool {
		head, exists, err := env.remote.RemoteBranchHead(t.Context(), env.repoURL, "main", git.AuthConfig{AuthType: "none"})
		return err == nil && exists && head != firstHead
	}, 5*time.Second, 25*time.Millisecond)
	require.Positive(t, env.scheduler.submitCount())
	require.NotEqual(t, firstHead, env.remoteHeadInternal(t, "main"))

	stored := env.reloadInternal(t, syncRecord.ID)
	require.NotNil(t, stored.LastBackupAt)
	assert.False(t, stored.BackupPending)
	assert.Equal(t, gitops.BackupStateBackedUp, stored.BackupState())
}

func TestGitOpsBackup_SaveSignalOnlyMarksPendingWhenAutoSyncIsOff(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{})
	head := env.remoteHeadInternal(t, "main")

	autoSync := false
	_, err := env.service.UpdateSync(t.Context(), "0", syncRecord.ID, gitops.UpdateSyncRequest{AutoSync: &autoSync}, common.SystemUser)
	require.NoError(t, err)

	env.service.backups.debounce = 50 * time.Millisecond
	env.service.SubscribeProjectFileChanges(t.Context())

	writeBackupProjectFileInternal(t, env.projectPath, "compose.yaml", "services:\n  app:\n    image: nginx:1.29-alpine\n")
	env.service.projectService.FilesChanged().Publish(env.project.ID)

	require.Eventually(t, func() bool {
		return env.reloadInternal(t, syncRecord.ID).BackupPending
	}, 5*time.Second, 25*time.Millisecond)

	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, head, env.remoteHeadInternal(t, "main"))

	stored := env.reloadInternal(t, syncRecord.ID)
	assert.True(t, stored.BackupPending)
	assert.Equal(t, gitops.BackupStatePaused, stored.BackupState())
}

func TestGitOpsBackup_ReconcileInterruptedBackupsOnStartup(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{})

	require.NoError(t, env.db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).
		Update("last_sync_status", backupStatusRunning).Error)

	require.NoError(t, env.service.ReconcileInterruptedBackupsOnStartup(t.Context()))

	stored := env.reloadInternal(t, syncRecord.ID)
	require.NotNil(t, stored.LastSyncStatus)
	assert.Equal(t, "failed", *stored.LastSyncStatus)
	assert.True(t, stored.BackupPending)
	assert.NotNil(t, stored.BackupPendingSince)
	assert.Equal(t, gitops.BackupStateFailed, stored.BackupState())
}

func TestGitOpsBackup_DeletingProjectRemovesBackupSync(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	syncRecord := env.createBackupInternal(t, gitops.CreateSyncRequest{})

	require.NoError(t, env.service.projectService.DestroyProject(t.Context(), env.project.ID, true, false, common.SystemUser))

	var remaining int64
	require.NoError(t, env.db.Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).Count(&remaining).Error)
	assert.Zero(t, remaining)
}

func TestGitOpsDeploy_LinksExistingProject(t *testing.T) {
	env := setupGitOpsBackupTestServiceInternal(t)
	ctx := context.Background()
	remoteCompose := "services:\n  app:\n    image: nginx:1.28-alpine\n"
	env.pushRemoteCommitInternal(t, "main", "seed", map[string]string{"apps/demo/compose.yaml": remoteCompose})
	writeBackupProjectFileInternal(t, env.projectPath, ".env", "TOKEN=keep-me\n")

	created, err := env.service.CreateSync(ctx, "0", gitops.CreateSyncRequest{
		Name:         "deploy-existing",
		RepositoryID: "repo-backup",
		Branch:       "main",
		ComposePath:  "apps/demo/compose.yaml",
		ProjectID:    env.project.ID,
	}, common.User{ID: "user-1", Username: "tester"})
	require.NoError(t, err)
	require.NotNil(t, created.ProjectID)
	require.Equal(t, env.project.ID, *created.ProjectID)
	require.Equal(t, env.project.Name, created.ProjectName)

	var project projectpkg.Project
	require.NoError(t, env.db.Where("id = ?", env.project.ID).First(&project).Error)
	require.NotNil(t, project.GitOpsManagedBy)
	require.Equal(t, created.ID, *project.GitOpsManagedBy)

	compose, err := os.ReadFile(filepath.Join(env.projectPath, "compose.yaml"))
	require.NoError(t, err)
	require.Equal(t, remoteCompose, string(compose))
	envFile, err := os.ReadFile(filepath.Join(env.projectPath, ".env"))
	require.NoError(t, err)
	require.Contains(t, string(envFile), "TOKEN=keep-me")

	_, err = env.service.CreateSync(ctx, "0", gitops.CreateSyncRequest{
		Name:         "deploy-again",
		RepositoryID: "repo-backup",
		Branch:       "main",
		ComposePath:  "apps/demo/compose.yaml",
		ProjectID:    env.project.ID,
	}, common.User{ID: "user-1", Username: "tester"})
	require.ErrorIs(t, err, common.ErrConflict)
}
