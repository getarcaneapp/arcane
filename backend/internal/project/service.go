package project

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	lifecycletype "github.com/getarcaneapp/arcane/types/v2/lifecycle"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
	"go.getarcane.app/docker"
	dockertypes "go.getarcane.app/docker/types"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	updatertypes "go.getarcane.app/updater/types"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/deployment"
	projectdetails "github.com/getarcaneapp/arcane/backend/v2/internal/project/children/details"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/lifecycle"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/listing"
	projectsync "github.com/getarcaneapp/arcane/backend/v2/internal/project/children/sync"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/tags"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/update"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/workspace"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
)

const (
	maxConcurrentComposeReads = 8
	// inferredServiceCountBatchSize keeps one inferred count update under
	// SQLite's bound variable limit (three placeholders per id).
	inferredServiceCountBatchSize = 200
)

var (
	composeStopProjectServices = projects.ComposeStop
	composeUpProjectServices   = projects.ComposeUp
)

type ProjectService struct {
	composeCoordinator          projecttypes.ComposeCoordinator
	db                          *database.DB
	settingsService             *settings.SettingsService
	eventService                *event.EventService
	imageService                *image.ImageService
	dockerService               *dockerInternal.DockerClientService
	lifecycleService            *LifecycleService
	workspace                   *workspace.Service
	details                     *projectdetails.Service
	listing                     *listing.Service
	deployment                  *deployment.Service
	updates                     *update.Service
	containerRegistryService    *registry.ContainerRegistryService
	config                      *config.Config
	RegistryCredentialsProvider func(context.Context) ([]containerregistry.Credential, error)

	// syncMu serializes SyncProjectsFromFileSystem: its discovery walk and its
	// cleanup pass must not interleave with another run's.
	syncMu sync.Mutex

	composeNames  composeNameCache
	parsedCompose projecttypes.ComposeCache[*types.Project]
	// metaCache holds per-project icon/URL metadata, keyed by project ID. Deriving
	// it costs a full compose load (interpolation plus .env reads) and, for GitOps
	// projects, a gitops_syncs lookup — per project, on every list request.
	// Entries are validated by compose/include/env file mtimes rather than a TTL.
	metaCache projecttypes.ComposeCache[projects.ArcaneComposeMetadata]
	// composeIdentities caches filesystem-sync compose identities by project path,
	// so a sync only re-parses projects whose compose or env files changed.
	composeIdentities projecttypes.ComposeCache[projecttypes.ComposeIdentity]

	// FilesChanged fires with the project ID after project files are saved
	// through Arcane, so Git backups can react without polling.
	FilesChanged *concurrency.Signal[string]
}

// EnsureGitOpsProjectLinked persists the bidirectional GitOps/project binding
// and refreshes the compose-name cache as one domain operation.
func (s *ProjectService) EnsureGitOpsProjectLinked(ctx context.Context, gitOpsSync *GitOpsSync, project *Project) error {
	if gitOpsSync == nil || project == nil {
		return nil
	}
	if kit.FromPtr(project.GitOpsManagedBy) != "" && kit.FromPtr(project.GitOpsManagedBy) != gitOpsSync.ID {
		return fmt.Errorf("project %s is already managed by a different GitOps sync", project.ID)
	}

	if kit.FromPtr(gitOpsSync.ProjectID) != project.ID || kit.FromPtr(project.GitOpsManagedBy) != gitOpsSync.ID {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if kit.FromPtr(gitOpsSync.ProjectID) != project.ID {
				if err := tx.Model(&GitOpsSync{}).Where("id = ?", gitOpsSync.ID).Update("project_id", project.ID).Error; err != nil {
					return fmt.Errorf("failed to relink GitOps sync %s: %w", gitOpsSync.ID, err)
				}
			}
			if kit.FromPtr(project.GitOpsManagedBy) != gitOpsSync.ID {
				if err := tx.Model(&Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", gitOpsSync.ID).Error; err != nil {
					return fmt.Errorf("failed to relink project %s to GitOps sync %s: %w", project.ID, gitOpsSync.ID, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	*gitOpsSync = GitOpsSync{ //exhaustruct:enforce
		BaseModel:              gitOpsSync.BaseModel,
		Environment:            gitOpsSync.Environment,
		Repository:             gitOpsSync.Repository,
		ProjectID:              &project.ID,
		Project:                gitOpsSync.Project,
		SyncedFiles:            gitOpsSync.SyncedFiles,
		LastSyncAt:             gitOpsSync.LastSyncAt,
		LastSyncStatus:         gitOpsSync.LastSyncStatus,
		LastSyncError:          gitOpsSync.LastSyncError,
		LastSyncCommit:         gitOpsSync.LastSyncCommit,
		PreDeployScriptPath:    gitOpsSync.PreDeployScriptPath,
		PreDeployRunnerImage:   gitOpsSync.PreDeployRunnerImage,
		PreDeployEnv:           gitOpsSync.PreDeployEnv,
		PreDeployExtraMounts:   gitOpsSync.PreDeployExtraMounts,
		PreDeployLastRunAt:     gitOpsSync.PreDeployLastRunAt,
		PreDeployLastRunStatus: gitOpsSync.PreDeployLastRunStatus,
		PreDeployLastRunOutput: gitOpsSync.PreDeployLastRunOutput,
		Name:                   gitOpsSync.Name,
		EnvironmentID:          gitOpsSync.EnvironmentID,
		RepositoryID:           gitOpsSync.RepositoryID,
		Branch:                 gitOpsSync.Branch,
		ComposePath:            gitOpsSync.ComposePath,
		TargetType:             gitOpsSync.TargetType,
		ProjectName:            gitOpsSync.ProjectName,
		PreDeployNetworkMode:   gitOpsSync.PreDeployNetworkMode,
		SyncInterval:           gitOpsSync.SyncInterval,
		MaxSyncFiles:           gitOpsSync.MaxSyncFiles,
		MaxSyncTotalSize:       gitOpsSync.MaxSyncTotalSize,
		MaxSyncBinarySize:      gitOpsSync.MaxSyncBinarySize,
		PreDeployTimeoutSec:    gitOpsSync.PreDeployTimeoutSec,
		AutoSync:               gitOpsSync.AutoSync,
		SyncDirectory:          gitOpsSync.SyncDirectory,
		PullImageAfterSync:     gitOpsSync.PullImageAfterSync,
		RedeployAfterSync:      gitOpsSync.RedeployAfterSync,
		Mode:                   gitOpsSync.Mode,
		BackupDirectory:        gitOpsSync.BackupDirectory,
		BackupPaths:            gitOpsSync.BackupPaths,
		BackupOnSave:           gitOpsSync.BackupOnSave,
		BackupPending:          gitOpsSync.BackupPending,
		BackupPendingSince:     gitOpsSync.BackupPendingSince,
		BackupConflict:         gitOpsSync.BackupConflict,
		BackupFailureReason:    gitOpsSync.BackupFailureReason,
		LastBackupAt:           gitOpsSync.LastBackupAt,
		LastBackupSnapshot:     gitOpsSync.LastBackupSnapshot,
	}
	*project = Project{ //exhaustruct:enforce
		BaseModel:          project.BaseModel,
		Name:               project.Name,
		DirName:            project.DirName,
		Path:               project.Path,
		Status:             project.Status,
		StatusReason:       project.StatusReason,
		ServiceCount:       project.ServiceCount,
		RunningCount:       project.RunningCount,
		GitOpsManagedBy:    &gitOpsSync.ID,
		ComposeProjectName: project.ComposeProjectName,
		ImageRefsJSON:      project.ImageRefsJSON,
		BuildImageRefsJSON: project.BuildImageRefsJSON,
		IsArchived:         project.IsArchived,
		ArchivedAt:         project.ArchivedAt,
	}
	s.composeNames.put(projects.NormalizeProjectName(project.Name), project.ID)
	return nil
}

// ValidateComposeDirectory loads a staged compose tree with the same settings,
// Docker path mapping, and validation rules used by managed projects.
func (s *ProjectService) ValidateComposeDirectory(ctx context.Context, projectName, projectPath, composeFileName string) (int, error) {
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return 0, err
	}
	composeProject, err := projects.LoadComposeProject(
		ctx,
		filepath.Join(projectPath, composeFileName),
		projects.NormalizeProjectName(projectName),
		projectsDirectory,
		s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
		s.projectPathMapper(ctx),
		nil,
		nil,
		true, nil, nil, nil,
	)
	if err != nil {
		return 0, err
	}
	return len(composeProject.Services), nil
}

// CreateGitOpsManagedProject persists a promoted GitOps project, links both
// records, updates the compose-name cache, and records the creation event.
func (s *ProjectService) CreateGitOpsManagedProject(ctx context.Context, gitOpsSync *GitOpsSync, project *Project, actor usertypes.Actor, logEventOptions ...bool) error {
	if gitOpsSync == nil || project == nil {
		return errors.New("GitOps sync and project are required")
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(project).Error; err != nil {
			return fmt.Errorf("failed to create project: %w", err)
		}
		if err := tx.Model(&GitOpsSync{}).Where("id = ?", gitOpsSync.ID).Update("project_id", project.ID).Error; err != nil {
			return fmt.Errorf("failed to update sync with project ID: %w", err)
		}
		if err := tx.Model(&Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", gitOpsSync.ID).Error; err != nil {
			return fmt.Errorf("failed to mark project as GitOps-managed: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := s.EnsureGitOpsProjectLinked(ctx, gitOpsSync, project); err != nil {
		return err
	}
	if err := s.reconcileComposeTagsForProject(ctx, project); err != nil {
		slog.WarnContext(ctx, "failed to reconcile Compose project tags during GitOps project creation", "projectId", project.ID, "error", err)
	}
	if len(logEventOptions) == 0 || logEventOptions[0] {
		s.logProjectEvent(
			ctx, event.EventTypeProjectCreate, project.ID, project.Name, actor,
			database.JSON{"action": "create", "projectID": project.ID, "projectName": project.Name, "path": project.Path},
			"could not log project creation",
		)
	}
	return nil
}

// newProjectMetadataEnv resolves the shared inputs once and preloads
// the GitOps compose paths of every listed project in a single query.
func (s *ProjectService) newProjectMetadataEnv(ctx context.Context, projectsList []Project) *projectMetadataEnv {
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve projects directory for compose selection", "error", err)
	}
	env := &projectMetadataEnv{
		projectsDirectory:  projectsDirectory,
		autoInjectEnv:      s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
		settings:           s.settingsService.GetSettingsOrDefaults(ctx),
		gitOpsComposePaths: make(map[string]string),
		composeFiles:       make(map[string]string, len(projectsList)),
	}
	s.preloadGitOpsComposePaths(ctx, env, projectsList)
	return env
}

// preloadGitOpsComposePaths fetches the GitOps compose paths of the
// given projects in one query and merges them into env, so list paths can
// widen the preloaded set to the rows they end up enriching.
func (s *ProjectService) preloadGitOpsComposePaths(ctx context.Context, env *projectMetadataEnv, projectsList []Project) {
	syncIDs := make([]string, 0, len(projectsList))
	for _, proj := range projectsList {
		if id := gitOpsSyncID(&proj); id != "" {
			syncIDs = append(syncIDs, id)
		}
	}
	if len(syncIDs) == 0 {
		return
	}
	var syncRecords []GitOpsSync
	if err := s.db.WithContext(ctx).Select("id", "compose_path").Where("id IN ?", syncIDs).Find(&syncRecords).Error; err != nil {
		// Unloaded IDs fall back to a per-project lookup that surfaces the failure.
		slog.WarnContext(ctx, "failed to batch resolve GitOps compose paths", "error", err)
		return
	}
	for _, id := range syncIDs {
		env.gitOpsComposePaths[id] = ""
	}
	for _, record := range syncRecords {
		env.gitOpsComposePaths[record.ID] = record.ComposePath
	}
}

func NewProjectService(
	db *database.DB,
	settingsService *settings.SettingsService,
	eventService *event.EventService,
	imageService *image.ImageService,
	dockerService *dockerInternal.DockerClientService,
	buildService builder,
	lifecycleService *LifecycleService,
	containerRegistryService *registry.ContainerRegistryService,
	cfg *config.Config,
	kvService *kv.KVService,
	registryCredentialsProvider func(context.Context) ([]containerregistry.Credential, error),
) *ProjectService {
	var s *ProjectService
	s = &ProjectService{
		composeCoordinator: projects.NewCoordinator(projecttypes.ComposeCommands{Stop: composeStopProjectServices, Up: composeUpProjectServices, Create: projects.ComposeCreate}),
		db:                 db,
		settingsService:    settingsService,
		eventService:       eventService,
		imageService:       imageService,
		dockerService:      dockerService,
		lifecycleService:   lifecycleService,
		workspace:          workspace.New(cfg),
		details:            projectdetails.New(db, dockerService, imageService, settingsService),
		listing: listing.New(
			func(ctx context.Context) ([]container.Summary, error) { return s.details.ComposeContainers(ctx) },
			imageService,
			func(imageRefs []string, services []projecttypes.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) *projecttypes.UpdateInfo {
				return buildUpdateInfoSummary(imageRefs, projectdetails.MergeProjectContainerUpdateInfo(nil, services, scoped), nil)
			},
		),
		deployment: deployment.New(settingsService, imageService, dockerService, buildService),
		// Rename recovery compares the stored name and path against its journal and rolls the row back.
		updates: update.New(
			kvService,
			dockerService,
			containerRegistryService,
			func(ctx context.Context) ([]containerregistry.Credential, error) {
				return s.ResolveRegistryCredentials(ctx)
			},
			func(ctx context.Context, projectID string) (string, string, bool, error) {
				proj, found, err := s.FindProjectByID(ctx, projectID)
				if err != nil || !found {
					return "", "", found, err
				}
				return proj.Name, proj.Path, true, nil
			},
			func(ctx context.Context, journal *projecttypes.RenameJournal) error {
				return db.WithContext(ctx).Model(&Project{}).Where("id = ?", journal.ProjectID).Updates(map[string]any{
					"name": journal.OldName, "path": journal.OldPath, "dir_name": journal.OldDirName,
				}).Error
			},
		),
		containerRegistryService:    containerRegistryService,
		config:                      cfg,
		RegistryCredentialsProvider: registryCredentialsProvider,
		parsedCompose:               projects.NewParsedComposeCache(),
		metaCache:                   projects.NewComposeCache[projects.ArcaneComposeMetadata](1024, nil),
		composeIdentities:           projects.NewComposeCache[projecttypes.ComposeIdentity](2048, nil),
		FilesChanged:                concurrency.NewSignal[string](),
	}
	return s
}

// RecoverProjectRenameJournals replays renames interrupted by a restart.
func (s *ProjectService) RecoverProjectRenameJournals(ctx context.Context) error {
	return s.updates.RecoverAll(ctx)
}

func (s *ProjectService) ResolveRegistryCredentials(ctx context.Context) ([]containerregistry.Credential, error) {
	credentials, err := s.RegistryCredentialsProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("get enabled registry credentials: %w", err)
	}

	return credentials, nil
}

func (s *ProjectService) GetProjectsDirectory(ctx context.Context) (string, error) {
	projectsDir, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return "", err
	}

	return filepath.Clean(projectsDir), nil
}

func (s *ProjectService) getMutableProject(ctx context.Context, projectID string) (*Project, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if proj.IsArchived {
		return nil, common.Classify(common.ErrProjectArchived, errors.New("project is archived and must be unarchived before this action"))
	}
	return proj, nil
}

func (s *ProjectService) logProjectEvent(ctx context.Context, eventType event.EventType, projectID, projectName string, user usertypes.Actor, metadata database.JSON, action string) {
	if logErr := s.eventService.LogProjectEvent(ctx, eventType, projectID, projectName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project event", "action", action, "error", logErr)
	}
}

func (s *ProjectService) GetProjectRelativePath(ctx context.Context, projectPath string) string {
	projectsDir, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return ""
	}

	return listing.RelativePath(projectsDir, projectPath)
}

func (s *ProjectService) GetProjectFromDatabaseByID(ctx context.Context, id string) (*Project, error) {
	projectModel, found, err := s.FindProjectByID(ctx, id)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("request canceled or timed out")
		}
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	if !found {
		return nil, errors.New("project not found")
	}
	return projectModel, nil
}

// FindProjectByID reports a missing project as found=false instead of an error.
func (s *ProjectService) FindProjectByID(ctx context.Context, projectID string) (*Project, bool, error) {
	var project Project
	if err := s.db.WithContext(ctx).Where("id = ?", projectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &project, true, nil
}

func (s *ProjectService) GetProjectByComposeName(ctx context.Context, name string) (*Project, error) {
	if name == "" {
		return nil, errors.New("project name is empty")
	}

	normalized := projects.NormalizeProjectName(name)
	var proj Project
	err := s.db.WithContext(ctx).Where("name = ? OR name = ?", name, normalized).First(&proj).Error
	if err == nil {
		s.composeNames.put(normalized, proj.ID)
		return &proj, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to get project by name: %w", err)
	}

	for attempt := range 2 {
		if projectID, ok := s.composeNames.projectID(normalized).Get(); ok {
			var projectModel Project
			switch lookupErr := s.db.WithContext(ctx).Where("id = ?", projectID).First(&projectModel).Error; {
			case errors.Is(lookupErr, gorm.ErrRecordNotFound), lookupErr == nil && projects.NormalizeProjectName(projectModel.Name) != normalized:
				s.composeNames.invalidate(normalized)
			case errors.Is(lookupErr, context.Canceled), errors.Is(lookupErr, context.DeadlineExceeded):
				return nil, fmt.Errorf("request canceled or timed out: %w", lookupErr)
			case lookupErr != nil:
				return nil, fmt.Errorf("failed to get project by cached compose name: %w", lookupErr)
			default:
				return &projectModel, nil
			}
		}
		if attempt > 0 {
			break
		}
		var projectModels []Project
		if listErr := s.db.WithContext(ctx).Select("id", "name").Find(&projectModels).Error; listErr != nil {
			return nil, fmt.Errorf("failed to list projects by compose name: %w", listErr)
		}
		byName := make(map[string]string, len(projectModels))
		for _, projectModel := range projectModels {
			if normalizedName := projects.NormalizeProjectName(projectModel.Name); normalizedName != "" && byName[normalizedName] == "" {
				byName[normalizedName] = projectModel.ID
			}
		}
		s.composeNames.replace(byName)
	}

	return nil, common.Classify(common.ErrNotFound, fmt.Errorf("project not found: %s", name))
}

// ComposeServiceImage returns the effective image of serviceName in the
// Arcane project deployed as composeName.
func (s *ProjectService) ComposeServiceImage(ctx context.Context, composeName, serviceName string) (projectID, imageRef string, err error) {
	proj, err := s.GetProjectByComposeName(ctx, composeName)
	if err != nil {
		return "", "", err
	}
	effective, _, err := s.loadComposeProjectForProject(ctx, proj, nil, serviceName)
	if err != nil {
		return "", "", fmt.Errorf("load project %s: %w", proj.Name, err)
	}
	service, ok := effective.Services[serviceName]
	if !ok {
		return "", "", fmt.Errorf("service %s is not active in project %s", serviceName, proj.Name)
	}
	return proj.ID, service.Image, nil
}

// EnsureProjectPathUnderRoot normalizes a path outside the projects root to `<projectsRoot>/<dirName or sanitized name>`.
// When persist=true, it saves the updated project path to the database.
func (s *ProjectService) EnsureProjectPathUnderRoot(ctx context.Context, proj *Project, persist bool) error {
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return fmt.Errorf("failed to get projects directory: %w", err)
	}

	rootAbs, _ := filepath.Abs(projectsDirectory)
	projPathAbs, absErr := filepath.Abs(proj.Path)
	if projects.IsSafeSubdirectory(rootAbs, kit.Ternary(absErr == nil, projPathAbs, proj.Path)) {
		return nil
	}

	candidate := filepath.Join(projectsDirectory, kit.Ternary(strings.TrimSpace(kit.FromPtr(proj.DirName)) == "", projects.SanitizeProjectName(proj.Name), kit.FromPtr(proj.DirName)))
	slog.WarnContext(ctx, "Normalizing project path to projects root", "projectId", proj.ID, "oldPath", proj.Path, "newPath", candidate, "root", projectsDirectory)
	*proj = Project{ //exhaustruct:enforce
		BaseModel:          proj.BaseModel,
		Name:               proj.Name,
		DirName:            proj.DirName,
		Path:               candidate,
		Status:             proj.Status,
		StatusReason:       proj.StatusReason,
		ServiceCount:       proj.ServiceCount,
		RunningCount:       proj.RunningCount,
		GitOpsManagedBy:    proj.GitOpsManagedBy,
		ComposeProjectName: proj.ComposeProjectName,
		ImageRefsJSON:      proj.ImageRefsJSON,
		BuildImageRefsJSON: proj.BuildImageRefsJSON,
		IsArchived:         proj.IsArchived,
		ArchivedAt:         proj.ArchivedAt,
	}

	if persist {
		if saveErr := s.db.WithContext(ctx).Save(proj).Error; saveErr != nil {
			slog.WarnContext(ctx, "failed to persist normalized project path", "error", saveErr)
		}
	}
	return nil
}

func (s *ProjectService) projectPathMapper(ctx context.Context) *projects.PathMapper {
	return projects.NewPathMapperForConfiguredDirectory(
		ctx,
		s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"),
		"/app/data/projects",
		func() *client.Client {
			dockerClient, _ := s.dockerService.GetClient(ctx)
			return dockerClient
		}(),
	)
}

func (s *ProjectService) invalidateProjectCaches(projectID string) {
	s.parsedCompose.Invalidate(projectID)
	s.metaCache.Invalidate(projectID)
}

// ResolveProjectComposeFile returns the base compose file for a project, mirroring `docker compose`: COMPOSE_FILE
// in the merged environment wins, then a GitOps sync's configured compose path, then standard detection.
func (s *ProjectService) ResolveProjectComposeFile(ctx context.Context, proj *Project) (string, error) {
	return s.resolveProjectComposeFile(ctx, proj, nil)
}

// resolveProjectComposeFile is ResolveProjectComposeFile with the
// request-scoped inputs supplied by env; a nil env resolves them per call.
func (s *ProjectService) resolveProjectComposeFile(ctx context.Context, proj *Project, env *projectMetadataEnv) (string, error) {
	if proj == nil {
		return "", errors.New("project is nil")
	}
	return env.composeFile(proj.ID, func() (string, error) {
		projectsDirectory, preloadedComposePaths := func() (string, map[string]string) {
			if env != nil {
				return env.projectsDirectory, env.gitOpsComposePaths
			}
			dir, dirErr := s.GetProjectsDirectory(ctx)
			if dirErr != nil {
				// The .env.global layer is skipped for an empty projects directory; surface the misconfiguration.
				slog.WarnContext(ctx, "failed to resolve projects directory for compose selection", "projectId", proj.ID, "error", dirErr)
			}
			return dir, nil
		}()
		if files, selErr := projects.ComposeFileEnvSelection(ctx, projectsDirectory, proj.Path); selErr != nil {
			return "", selErr
		} else if len(files) > 0 {
			return files[0], nil
		}

		// A GitOps sync's compose path comes from the preloaded request map when available; a missing row resolves to "".
		composePath, err := func() (string, error) {
			if gitOpsSyncID(proj) == "" {
				return "", nil
			}
			if preloaded, ok := preloadedComposePaths[gitOpsSyncID(proj)]; ok {
				return preloaded, nil
			}
			var syncRecord GitOpsSync
			if err := s.db.WithContext(ctx).Select("compose_path").Where("id = ?", gitOpsSyncID(proj)).First(&syncRecord).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return "", fmt.Errorf("failed to resolve GitOps compose path for project %s: %w", proj.ID, err)
			}
			return syncRecord.ComposePath, nil
		}()
		if err != nil {
			return "", err
		}
		if composeFileName := strings.TrimSpace(filepath.Base(composePath)); composeFileName != "" && composeFileName != "." {
			// os.Stat rather than acfs: an imported project or a symlinked compose file may resolve outside the projects directory.
			info, statErr := os.Stat(filepath.Join(proj.Path, composeFileName))
			if statErr == nil && !info.IsDir() {
				return filepath.Join(proj.Path, composeFileName), nil
			}
			if statErr != nil && !os.IsNotExist(statErr) {
				return "", fmt.Errorf("failed to inspect GitOps compose file %s: %w", filepath.Join(proj.Path, composeFileName), statErr)
			}
		}

		composeFile, err := projects.DetectComposeFile(ctx, projectsDirectory, proj.Path)
		if err != nil {
			if errors.Is(err, common.ErrProjectEnvUnreadable) {
				return "", err
			}
			return "", common.Classify(common.ErrProjectComposeFileNotFound, fmt.Errorf("Project compose file not found: %w", err))
		}
		return composeFile, nil
	})
}

// loadComposeProjectForProject loads the executable compose model for proj. prepare is optional and runs before
// host path translation; deployment paths use it to create missing bind directories, read paths pass nil.
func (s *ProjectService) loadComposeProjectForProject(ctx context.Context, proj *Project, prepare projects.PrepareProjectFunc, services ...string) (*types.Project, string, error) {
	composeFileFullPath, err := s.ResolveProjectComposeFile(ctx, proj)
	if err != nil {
		return nil, "", err
	}

	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	projectsDirectory, dirErr := projects.GetProjectsDirectory(ctx, cfg.ProjectsDirectory.Value)
	if dirErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", dirErr)
	}
	composeProject, loadErr := projects.LoadComposeProject(
		ctx,
		composeFileFullPath,
		projects.NormalizeProjectName(proj.Name),
		kit.Ternary(dirErr == nil, projectsDirectory, "/app/data/projects"),
		kit.ParseOrDefault(cfg.AutoInjectEnv.Value, false, strconv.ParseBool),
		s.projectPathMapper(ctx),
		nil, nil, false, nil, services, prepare,
	)
	if loadErr != nil {
		return nil, "", loadErr
	}
	return composeProject, composeFileFullPath, nil
}

func (s *ProjectService) getCachedComposeProject(ctx context.Context, proj *Project, env *projectMetadataEnv) (*types.Project, error) {
	if proj == nil {
		return nil, errors.New("project is nil")
	}
	cfg := func() *settings.Settings {
		if env == nil {
			return s.settingsService.GetSettingsOrDefaults(ctx)
		}
		return env.settings
	}()
	composePath, err := s.resolveProjectComposeFile(ctx, proj, env)
	if err != nil {
		return nil, err
	}
	projectsDirectory, dirErr := projects.GetProjectsDirectory(ctx, cfg.ProjectsDirectory.Value)
	if dirErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", dirErr)
	}
	return projects.LoadCachedComposeProject(
		ctx,
		s.parsedCompose,
		proj.ID,
		proj.Path,
		composePath,
		projects.NormalizeProjectName(proj.Name),
		kit.Ternary(dirErr == nil, projectsDirectory, "/app/data/projects"),
		kit.ParseOrDefault(cfg.AutoInjectEnv.Value, false, strconv.ParseBool),
		s.projectPathMapper(ctx),
	)
}

func (s *ProjectService) refreshComposeProjectName(ctx context.Context, proj *Project) {
	meta, err := func() (projecttypes.ComposeIdentity, error) {
		cfg := s.settingsService.GetSettingsOrDefaults(ctx)
		projectsDirectory, dirErr := projects.GetProjectsDirectory(ctx, cfg.ProjectsDirectory.Value)
		if dirErr != nil {
			return projecttypes.ComposeIdentity{}, dirErr
		}
		autoInjectEnv := kit.ParseOrDefault(cfg.AutoInjectEnv.Value, false, strconv.ParseBool)
		return projectsync.LoadComposeMetadata(ctx, s.composeIdentities, proj.Path, cmp.Or(kit.FromPtr(proj.DirName), proj.Name), projectsDirectory, autoInjectEnv, s.projectPathMapper(ctx))
	}()
	if err != nil {
		if errors.Is(err, common.ErrProjectEnvUnreadable) {
			slog.DebugContext(ctx, "skipped compose project name refresh; project env is unreadable", "projectId", proj.ID, "path", proj.Path, "error", err)
			return
		}
		slog.WarnContext(ctx, "failed to refresh compose project name", "projectId", proj.ID, "path", proj.Path, "error", err)
		return
	}

	name := kit.Ternary(
		(meta.ExplicitProjectName || projects.NormalizeProjectName(proj.Name) != proj.Name) && meta.ResolvedProjectName != "",
		meta.ResolvedProjectName,
		proj.Name,
	)
	if name == proj.Name && mo.PointerToOption(proj.ComposeProjectName) == mo.PointerToOption(meta.ComposeProjectName) {
		return
	}
	if persistComposeNameErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", proj.ID).Updates(map[string]any{
		"name":                 name,
		"compose_project_name": meta.ComposeProjectName,
		"updated_at":           time.Now(),
	}).Error; persistComposeNameErr != nil {
		slog.WarnContext(ctx, "failed to persist refreshed compose project name", "projectId", proj.ID, "error", persistComposeNameErr)
		return
	}

	*proj = Project{ //exhaustruct:enforce
		BaseModel:          proj.BaseModel,
		Name:               name,
		DirName:            proj.DirName,
		Path:               proj.Path,
		Status:             proj.Status,
		StatusReason:       proj.StatusReason,
		ServiceCount:       proj.ServiceCount,
		RunningCount:       proj.RunningCount,
		GitOpsManagedBy:    proj.GitOpsManagedBy,
		ComposeProjectName: meta.ComposeProjectName,
		ImageRefsJSON:      proj.ImageRefsJSON,
		BuildImageRefsJSON: proj.BuildImageRefsJSON,
		IsArchived:         proj.IsArchived,
		ArchivedAt:         proj.ArchivedAt,
	}
}

// LifecycleService runs a GitOps sync's pre-deploy script in a throwaway container.
// The script is trusted like the repo's compose.yaml; configuring its path is the trust event.
type LifecycleService struct {
	db              *database.DB
	settingsService *settings.SettingsService
	eventService    *event.EventService
	hooks           *lifecycle.Service
}

// NewLifecycleService constructs a LifecycleService wired against shared
// infrastructure. The Docker client is obtained lazily on each hook run via
// dockerService.GetClient so reconnects are transparent. Runner image pulls go
// through imageService so configured registry credentials apply.
func NewLifecycleService(
	db *database.DB,
	settingsService *settings.SettingsService,
	eventService *event.EventService,
	dockerService *dockerInternal.DockerClientService,
	imageService *image.ImageService,
) *LifecycleService {
	return &LifecycleService{
		db:              db,
		settingsService: settingsService,
		eventService:    eventService,
		hooks:           lifecycle.New(settingsService, dockerService, imageService),
	}
}

// runPreDeploy runs the sync's pre-deploy hook, if configured and enabled. Any
// failure aborts the deploy; every run records its last-run state on the sync.
func (s *LifecycleService) runPreDeploy(ctx context.Context, project *Project, actor usertypes.Actor) error {
	if gitOpsSyncID(project) == "" || !s.settingsService.GetBoolSetting(ctx, "lifecycleEnabled", false) {
		return nil
	}

	var syncRecord GitOpsSync
	switch err := s.db.WithContext(ctx).Where("project_id = ?", project.ID).First(&syncRecord).Error; {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("failed to load gitops sync for lifecycle hook: %w", err)
	}
	if strings.TrimSpace(kit.FromPtr(syncRecord.PreDeployScriptPath)) == "" {
		return nil
	}

	runnerImage := cmp.Or(
		strings.TrimSpace(kit.FromPtr(syncRecord.PreDeployRunnerImage)),
		strings.TrimSpace(s.settingsService.GetStringSetting(ctx, "lifecycleDefaultRunnerImage", "alpine:latest")),
	)
	if runnerImage == "" {
		return fmt.Errorf("pre-deploy script %q is configured but no runner image is set on the GitOps sync or lifecycleDefaultRunnerImage setting", *syncRecord.PreDeployScriptPath)
	}

	scriptPath := strings.TrimSpace(*syncRecord.PreDeployScriptPath)
	if validateErr := lifecycle.ValidateScriptPath(ctx, project.Path, scriptPath); validateErr != nil {
		return fmt.Errorf("invalid pre-deploy script path: %w", validateErr)
	}

	hookEnv, err := ParseEnvText(syncRecord.PreDeployEnv)
	if err != nil {
		return fmt.Errorf("invalid lifecycle env config: %w", err)
	}
	extraMounts, err := ParseExtraMountsText(syncRecord.PreDeployExtraMounts)
	if err != nil {
		return fmt.Errorf("invalid lifecycle extra mounts config: %w", err)
	}

	maxTimeoutSec := s.settingsService.GetIntSetting(ctx, "lifecycleMaxTimeoutSec", lifecycletype.DefaultMaxTimeoutSec)
	timeout := time.Duration(kit.Ternary(
		maxTimeoutSec > 0,
		min(kit.Ternary(syncRecord.PreDeployTimeoutSec > 0, syncRecord.PreDeployTimeoutSec, lifecycletype.DefaultTimeoutSec), maxTimeoutSec),
		kit.Ternary(syncRecord.PreDeployTimeoutSec > 0, syncRecord.PreDeployTimeoutSec, lifecycletype.DefaultTimeoutSec),
	)) * time.Second
	slog.InfoContext(ctx, "running pre-deploy lifecycle hook",
		"projectId", project.ID,
		"syncId", syncRecord.ID,
		"scriptPath", scriptPath,
		"runnerImage", runnerImage,
		"timeoutSec", int(timeout/time.Second),
	)

	start := time.Now()
	stdoutContent, stderrContent, exitCode, runErr := s.hooks.RunScript(ctx, runnerImage, project.Path, scriptPath, hookEnv, extraMounts, syncRecord.PreDeployNetworkMode, timeout, actor)
	durationMs := time.Since(start).Milliseconds()
	status := func() string {
		switch {
		case errors.Is(runErr, context.DeadlineExceeded):
			return lifecycle.LifecycleStatusTimeout
		case runErr != nil || exitCode != 0:
			return lifecycle.LifecycleStatusFailed
		default:
			return lifecycle.LifecycleStatusSuccess
		}
	}()

	// Each stream is already bounded; cutting the combined output again could split a UTF-8 codepoint.
	if persistErr := s.db.WithContext(context.WithoutCancel(ctx)).Model(&GitOpsSync{}).Where("id = ?", syncRecord.ID).Updates(map[string]any{
		"pre_deploy_last_run_at":     start,
		"pre_deploy_last_run_status": status,
		"pre_deploy_last_run_output": lifecycle.CombineLifecycleOutput(stdoutContent, stderrContent),
	}).Error; persistErr != nil {
		slog.WarnContext(ctx, "failed to persist lifecycle last-run state", "syncId", syncRecord.ID, "error", persistErr)
	}

	if _, eventErr := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:     event.EventTypeLifecycleExecute,
		Severity: kit.Ternary(status == lifecycle.LifecycleStatusSuccess, event.EventSeveritySuccess, event.EventSeverityWarning),
		Title: kit.Ternary(
			status == lifecycle.LifecycleStatusSuccess,
			"Pre-deploy lifecycle hook succeeded: "+project.Name,
			fmt.Sprintf("Pre-deploy lifecycle hook %s: %s", status, project.Name),
		),
		Description: kit.Ternary(
			status != lifecycle.LifecycleStatusSuccess && runErr != nil,
			fmt.Sprint(runErr),
			fmt.Sprintf("Script %s exited with code %d in %dms", *syncRecord.PreDeployScriptPath, exitCode, durationMs),
		),
		ResourceType:  new("project"),
		ResourceID:    new(project.ID),
		ResourceName:  new(project.Name),
		EnvironmentID: new(syncRecord.EnvironmentID),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		Metadata: database.JSON{
			"scriptPath":   *syncRecord.PreDeployScriptPath,
			"runnerImage":  runnerImage,
			"exitCode":     exitCode,
			"durationMs":   durationMs,
			"gitopsSyncId": syncRecord.ID,
			"status":       status,
		},
	}); eventErr != nil {
		slog.WarnContext(ctx, "failed to emit lifecycle.execute event", "syncId", syncRecord.ID, "error", eventErr)
	}

	if runErr != nil {
		return runErr
	}
	if exitCode != 0 {
		return fmt.Errorf("pre-deploy script exited with status %d", exitCode)
	}
	return nil
}

// ParseEnvText reads admin-configured env config as the
// same KEY=VALUE text format used by .env files: one entry per line, blank
// and "#"-prefixed lines ignored, keys must match POSIX identifier syntax.
// Reuses the strict parser also used for stdout-capture.
func ParseEnvText(raw *string) (map[string]string, error) {
	if strings.TrimSpace(kit.FromPtr(raw)) == "" {
		return map[string]string{}, nil
	}
	env, err := lifecycle.ParseKeyValueEnv(*raw)
	if err != nil {
		return nil, fmt.Errorf("invalid env entry: %w", err)
	}
	return env, nil
}

// ParseExtraMountsText reads admin-configured bind mounts
// in docker-CLI "src:tgt[:ro|:rw]" form, one per line. Blank and
// "#"-prefixed lines are ignored. Both source and target must be absolute
// paths; mode defaults to read-write.
func ParseExtraMountsText(raw *string) ([]lifecycletype.ExtraMount, error) {
	if strings.TrimSpace(kit.FromPtr(raw)) == "" {
		return nil, nil
	}

	var mounts []lifecycletype.ExtraMount
	scanner := bufio.NewScanner(strings.NewReader(*raw))
	scanner.Buffer(make([]byte, 0, 4*1024), 64*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf("line %d: expected src:tgt[:ro|:rw], got %q", lineNum, line)
		}

		if len(parts) == 3 && parts[2] != "ro" && parts[2] != "rw" {
			return nil, fmt.Errorf("line %d: invalid mode %q (expected \"ro\" or \"rw\")", lineNum, parts[2])
		}
		mount := lifecycletype.ExtraMount{Source: parts[0], Target: parts[1], Readonly: len(parts) == 3 && parts[2] == "ro"}

		// Mount sources/targets are interpreted by the Docker daemon as POSIX
		// host/container paths, so use path.IsAbs to avoid host-OS quirks
		// (filepath.IsAbs("/x") returns false on Windows).
		if !path.IsAbs(filepath.ToSlash(mount.Source)) {
			return nil, fmt.Errorf("line %d: source %q must be an absolute path", lineNum, mount.Source)
		}
		if !path.IsAbs(filepath.ToSlash(mount.Target)) {
			return nil, fmt.Errorf("line %d: target %q must be an absolute path", lineNum, mount.Target)
		}

		mounts = append(mounts, mount)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read extra mounts config: %w", err)
	}
	return mounts, nil
}

func (s *ProjectService) UpdateProjectServices(ctx context.Context, projectID string, servicesToUpdate []string, user usertypes.Actor, discoverTags bool) error {
	proj, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return err
	}
	if discoverTags {
		effective, _, loadErr := s.loadComposeProjectForProject(ctx, proj, nil, servicesToUpdate...)
		if loadErr != nil {
			return fmt.Errorf("load project for service image checks: %w", loadErr)
		}
		changes, changesErr := s.updates.ImageChanges(ctx, effective, gitOpsSyncID(proj) != "")
		if changesErr != nil {
			return changesErr
		}
		if len(changes) > 0 {
			if _, saveErr := s.SaveProjectServiceImages(ctx, projectID, changes); saveErr != nil {
				return saveErr
			}
		}
	}
	previousStatus := proj.Status

	// 1. Load project
	prepare := deployment.PrepareProjectBindDirectories(proj.Path)
	compProj, _, err := s.loadComposeProjectForProject(ctx, proj, prepare, servicesToUpdate...)
	if err != nil {
		return fmt.Errorf("failed to load compose project: %w", err)
	}
	dependents, stoppedDependents := s.deployment.NamespaceDependents(ctx, compProj, servicesToUpdate)
	if len(dependents)+len(stoppedDependents) > 0 {
		slog.InfoContext(ctx, "recreating namespace dependents with updated services", "projectId", projectID, "services", servicesToUpdate, "dependents", dependents, "stoppedDependents", stoppedDependents)
		if compProj, _, err = s.loadComposeProjectForProject(ctx, proj, prepare, slices.Concat(servicesToUpdate, dependents, stoppedDependents)...); err != nil {
			return fmt.Errorf("failed to load compose project with dependents: %w", err)
		}
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	// 2. Set status to deploying/restarting
	if updateProjectStatusErr := s.updateProjectStatus(ctx, projectID, ProjectStatusDeploying); updateProjectStatusErr != nil {
		return updateProjectStatusErr
	}

	credentials, err := s.ResolveRegistryCredentials(ctx)
	if err != nil {
		if statusErr := s.updateProjectStatus(ctx, projectID, previousStatus); statusErr != nil {
			slog.ErrorContext(ctx, "UpdateProjectServices: failed to restore project status after credential lookup failure", "projectId", projectID, "error", statusErr)
		}
		return fmt.Errorf("resolve registry credentials: %w", err)
	}

	authConfigs, authErr := s.containerRegistryService.GetAllRegistryAuthConfigs(ctx)
	if authErr != nil {
		slog.WarnContext(ctx, "failed to load registry auth for compose pulls", "error", authErr)
	}
	progressWriter, _ := ctx.Value(dockertypes.ProgressWriterKey{}).(io.Writer)
	if updateServicesErr := s.composeCoordinator.UpdateServices(ctx, projecttypes.ComposeServiceUpdate{
		Project: compProj, Services: servicesToUpdate, Dependents: dependents, StoppedDependents: stoppedDependents,
		Images: s.deployment.ImageOperations(&user, credentials), Progress: progressWriter,
		AuthConfigs: authConfigs, WaitTimeout: timeouts.GetDuration(s.settingsService.GetSettingsConfig().DeployWaitTimeout.AsInt(), timeouts.DefaultDeployWait),
		RestoreBeforeMutation: func(ctx context.Context) {
			if statusErr := s.updateProjectStatus(ctx, projectID, previousStatus); statusErr != nil {
				slog.ErrorContext(ctx, "failed to restore project status before service update", "projectId", projectID, "error", statusErr)
			}
		},
		Recover: func(ctx context.Context) {
			services, servicesErr := s.projectServices(ctx, projectID)
			if servicesErr != nil {
				slog.WarnContext(ctx, "failed to inspect project services after deploy failure", "projectId", projectID, "error", servicesErr)
			} else {
				serviceCount, runningCount := listing.ServiceCounts(services)
				updateErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
					"status":        ProjectStatus(listing.ProjectStatus(services)),
					"service_count": serviceCount,
					"running_count": runningCount,
					"updated_at":    time.Now(),
				}).Error
				if updateErr == nil {
					return
				}
				slog.WarnContext(ctx, "failed to restore project status after deploy failure", "projectId", projectID, "error", updateErr)
			}
			if updateErr := s.updateProjectStatus(ctx, projectID, ProjectStatusStopped); updateErr != nil {
				slog.WarnContext(ctx, "failed to set stopped status after deploy failure", "projectId", projectID, "error", updateErr)
			}
		},
	}); updateServicesErr != nil {
		return updateServicesErr
	}

	// 6. Finalize status
	if updateProjectStatusandCountsErr := s.updateProjectStatusAndCounts(ctx, projectID, ProjectStatusRunning); updateProjectStatusandCountsErr != nil {
		return updateProjectStatusandCountsErr
	}

	metadata := database.JSON{
		"action":      "update_services",
		"projectID":   projectID,
		"projectName": proj.Name,
		"services":    append([]string(nil), servicesToUpdate...),
	}
	if len(dependents) > 0 {
		metadata["dependents"] = dependents
	}
	if len(stoppedDependents) > 0 {
		metadata["stoppedDependents"] = stoppedDependents
	}
	s.logProjectEvent(ctx, event.EventTypeProjectUpdate, projectID, proj.Name, user, metadata, "could not log project service update action")

	return nil
}

func (s *ProjectService) ArchiveProject(ctx context.Context, projectID string, user usertypes.Actor) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	if proj.IsArchived {
		return nil
	}

	// Gate on live Docker state, not the persisted status row, which can go
	// stale when containers are stopped outside an Arcane project action.
	// A project without a compose file cannot have managed containers running
	// and is a prime archive candidate, so it is allowed through.
	services, servicesErr := s.projectServices(ctx, projectID)
	switch {
	case servicesErr != nil && !errors.Is(servicesErr, common.ErrProjectComposeFileNotFound):
		return fmt.Errorf("cannot verify project is stopped before archiving: %w", servicesErr)
	case servicesErr == nil:
		if _, running := listing.ServiceCounts(services); running > 0 {
			return common.Classify(common.ErrProjectMustBeStopped, errors.New("project must be stopped before archiving"))
		}
	}

	if archiveProjectErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"is_archived": true,
		"archived_at": time.Now(),
	}).Error; archiveProjectErr != nil {
		return fmt.Errorf("failed to archive project: %w", archiveProjectErr)
	}

	s.logProjectEvent(
		ctx, event.EventTypeProjectUpdate, projectID, proj.Name, user,
		database.JSON{"action": "archived", "projectID": projectID, "projectName": proj.Name},
		"could not log project archive action",
	)

	return nil
}

func (s *ProjectService) UnarchiveProject(ctx context.Context, projectID string, user usertypes.Actor) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	if !proj.IsArchived {
		return nil
	}

	if unarchiveProjectErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"is_archived": false,
		"archived_at": gorm.Expr("NULL"),
	}).Error; unarchiveProjectErr != nil {
		return fmt.Errorf("failed to unarchive project: %w", unarchiveProjectErr)
	}

	s.logProjectEvent(
		ctx, event.EventTypeProjectUpdate, projectID, proj.Name, user,
		database.JSON{"action": "unarchived", "projectID": projectID, "projectName": proj.Name},
		"could not log project unarchive action",
	)

	return nil
}

func (s *ProjectService) DeployProject(ctx context.Context, projectID string, user usertypes.Actor, options *projecttypes.DeployOptions) error {
	projectFromDb, err := s.getMutableProject(ctx, projectID)
	switch {
	case errors.Is(err, common.ErrProjectArchived):
		return err
	case err != nil:
		return fmt.Errorf("failed to get project: %w", err)
	}
	if _, resolveProjectComposeFileErr := s.ResolveProjectComposeFile(ctx, projectFromDb); resolveProjectComposeFileErr != nil {
		return resolveProjectComposeFileErr
	}

	if updateProjectStatusErr := s.updateProjectStatus(ctx, projectID, ProjectStatusDeploying); updateProjectStatusErr != nil {
		return fmt.Errorf("failed to update project status to deploying: %w", updateProjectStatusErr)
	}
	var closeSuppression func()
	defer func() {
		if closeSuppression != nil {
			closeSuppression()
		}
	}()

	authConfigs, authErr := s.containerRegistryService.GetAllRegistryAuthConfigs(ctx)
	if authErr != nil {
		slog.WarnContext(ctx, "failed to load registry auth for compose pulls", "error", authErr)
	}
	progressWriter, _ := ctx.Value(dockertypes.ProgressWriterKey{}).(io.Writer)
	projectModel, err := s.composeCoordinator.Deploy(ctx, projecttypes.ComposeDeployment{
		ProjectID: projectID, ProjectPath: projectFromDb.Path, Options: options,
		DefaultPullPolicy: s.settingsService.GetStringSetting(ctx, "defaultDeployPullPolicy", "missing"),
		GitOpsManaged:     gitOpsSyncID(projectFromDb) != "",
		WaitTimeout:       timeouts.GetDuration(s.settingsService.GetSettingsConfig().DeployWaitTimeout.AsInt(), timeouts.DefaultDeployWait),
		AuthConfigs:       authConfigs,
		Progress:          progressWriter,
		PreDeploy: func(ctx context.Context) error {
			return s.lifecycleService.runPreDeploy(ctx, projectFromDb, user)
		},
		Load: func(ctx context.Context) (*types.Project, error) {
			model, _, loadComposeProjectForProjectErr := s.loadComposeProjectForProject(ctx, projectFromDb, deployment.PrepareProjectBindDirectories(projectFromDb.Path))
			if loadComposeProjectForProjectErr == nil {
				closeSuppression = s.eventService.BeginComposeSuppressionWindow(model.Name)
			}
			return model, loadComposeProjectForProjectErr
		},
		ResolveImages: func(ctx context.Context) (projecttypes.ComposeImageOperations, error) {
			credentials, resolveRegistryCredentialsErr := s.ResolveRegistryCredentials(ctx)
			operations := s.deployment.ImageOperations(&user, credentials)
			return projecttypes.ComposeImageOperations{
				Exists:     operations.Exists,
				LastTagged: operations.LastTagged,
				// Deployment pulls are recorded as system actions; builds retain the requesting actor.
				Pull:          s.deployment.ImageOperations(nil, credentials).Pull,
				Build:         operations.Build,
				BuildProvider: operations.BuildProvider,
			}, resolveRegistryCredentialsErr
		},
		Recover: func(ctx context.Context) {
			services, servicesErr := s.projectServices(ctx, projectID)
			if servicesErr != nil {
				slog.WarnContext(ctx, "failed to inspect project services after deploy failure", "projectId", projectID, "error", servicesErr)
			} else {
				serviceCount, runningCount := listing.ServiceCounts(services)
				updateErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
					"status":        ProjectStatus(listing.ProjectStatus(services)),
					"service_count": serviceCount,
					"running_count": runningCount,
					"updated_at":    time.Now(),
				}).Error
				if updateErr == nil {
					return
				}
				slog.WarnContext(ctx, "failed to restore project status after deploy failure", "projectId", projectID, "error", updateErr)
			}
			if updateErr := s.updateProjectStatus(ctx, projectID, ProjectStatusStopped); updateErr != nil {
				slog.WarnContext(ctx, "failed to set stopped status after deploy failure", "projectId", projectID, "error", updateErr)
			}
		},
	})
	if err != nil {
		return err
	}

	s.logProjectEvent(
		ctx, event.EventTypeProjectDeploy, projectID, projectModel.Name, user,
		database.JSON{"action": "deploy", "projectID": projectID, "projectName": projectModel.Name},
		"could not log project deployment action",
	)

	err = s.updateProjectStatusAndCounts(ctx, projectID, ProjectStatusRunning)
	if err != nil {
		slog.ErrorContext(ctx, "failed to update project status and counts after deploy", "projectId", projectID, "error", err)
	}
	return err
}

func (s *ProjectService) DownProject(ctx context.Context, projectID string, user usertypes.Actor) error {
	projectFromDb, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return err
	}

	proj, _, lerr := s.loadComposeProjectForProject(ctx, projectFromDb, nil)
	if lerr != nil {
		_ = s.updateProjectStatus(ctx, projectID, ProjectStatusRunning)
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	if updateProjectStatusErr := s.updateProjectStatus(ctx, projectID, ProjectStatusStopped); updateProjectStatusErr != nil {
		return fmt.Errorf("failed to update project status to stopping: %w", updateProjectStatusErr)
	}

	defer s.eventService.BeginComposeSuppressionWindow(proj.Name)()

	if composeDownErr := projects.ComposeDown(ctx, proj, false); composeDownErr != nil {
		_ = s.updateProjectStatus(ctx, projectID, ProjectStatusRunning)
		return fmt.Errorf("failed to bring down project: %w", composeDownErr)
	}

	s.logProjectEvent(ctx, event.EventTypeProjectStop, projectID, projectFromDb.Name, user, database.JSON{
		"action":      "down",
		"projectID":   projectID,
		"projectName": projectFromDb.Name,
	}, "could not log project down action")

	return s.updateProjectStatusAndCounts(ctx, projectID, ProjectStatusStopped)
}

// CreateProject creates a project's directory, files, and DB row. A directory
// collision appends "-N" unless allowNameSuffix is false, which returns projects.ErrProjectDirExists.
func (s *ProjectService) CreateProject(
	ctx context.Context,
	name, composeContent string,
	envContent *string,
	manifest projecttypes.CreateProjectWorkspaceManifest,
	uploads map[int][]byte,
	uiTags []string,
	uiTagColors map[string]projecttypes.TagColor,
	user usertypes.Actor,
	allowNameSuffixOptions ...bool,
) (*Project, error) {
	normalizedUITags, err := projects.NormalizeProjectTags(uiTags)
	if err != nil {
		return nil, fmt.Errorf("invalid project tags: %w", err)
	}
	normalizedTagColors, err := tags.NormalizeProjectTagColors(uiTagColors)
	if err != nil {
		return nil, fmt.Errorf("invalid project tag colors: %w", err)
	}
	allowNameSuffix := len(allowNameSuffixOptions) == 0 || allowNameSuffixOptions[0]
	// A top-level `name:` in the compose file is authoritative over the submitted project name.
	name = cmp.Or(projects.ComposeContentProjectName(composeContent), name)

	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	// Held through the DB insert so concurrent creates and filesystem syncs
	// cannot claim the directory while it is half written.
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	basePath := filepath.Join(projectsDirectory, projects.SanitizeProjectName(name))
	projectPath, folderName, reused, err := kit.Ternary(allowNameSuffix, func() (string, string, bool, error) {
		// Projects at or nested beneath an existing directory claim it.
		return projects.CreateUniqueDir(ctx, projectsDirectory, basePath, name, utils.DirPerm, func(dir string) (bool, error) {
			nestedPattern := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(dir+string(filepath.Separator)) + "%"
			var claims int64
			countErr := s.db.WithContext(ctx).Model(&Project{}).Where("path = ? OR path LIKE ? ESCAPE '\\'", dir, nestedPattern).Count(&claims).Error
			return claims > 0, countErr
		})
	}, func() (string, string, bool, error) {
		exactPath, exactFolder, exactErr := projects.CreateExactDir(ctx, projectsDirectory, basePath, name, utils.DirPerm)
		return exactPath, exactFolder, false, exactErr
	})()
	if err != nil {
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}
	projectLogical, err := acfs.LogicalPath(projectsDirectory, projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve created project directory: %w", err)
	}

	// A new directory is removed on failure. A reused one only gains files:
	// every path creation can write must be absent (an existing .env file is
	// kept as is), so the backup records only absent paths and rollback removes just
	// what was added. Blank env content keeps an existing .env; different
	// content conflicts with it.
	cleanupCtx := context.WithoutCancel(ctx)
	undo := func() error { return acfs.RemoveAll(cleanupCtx, projectsDirectory, projectLogical) }
	if reused {
		targets := []string{projects.DefaultComposeFileName, projects.EffectiveEnvFileName}
		for _, change := range manifest.FileChanges {
			targets = append(targets, workspace.ChangeTargetPaths(change)...)
		}
		var absentPaths []string
		for _, target := range targets {
			entry, statErr := acfs.Stat(ctx, projectPath, "/"+target, false)
			switch {
			case errors.Is(statErr, os.ErrNotExist):
				absentPaths = append(absentPaths, target)
			case statErr != nil:
				return nil, workspace.WrapProjectWorkspaceError(statErr)
			case target != projects.EffectiveEnvFileName || entry.IsDirectory:
				return nil, common.Classify(common.ErrProjectWorkspaceConflict, fmt.Errorf("%s already exists in existing directory %q and cannot be changed", target, folderName))
			}
		}
		envContent = kit.Ternary(strings.TrimSpace(kit.FromPtr(envContent)) == "", nil, envContent)
		if envContent != nil {
			currentEnv, readEnvErr := os.ReadFile(filepath.Join(projectPath, projects.EffectiveEnvFileName))
			if !errors.Is(readEnvErr, os.ErrNotExist) && (readEnvErr != nil || string(currentEnv) != *envContent) {
				return nil, common.Classify(common.ErrProjectWorkspaceConflict, fmt.Errorf(".env already exists in %q with different content; clear the environment content to keep it", folderName))
			}
		}
		backup, cleanupBackup, backupProjectDirectoryErr := projects.BackupProjectDirectory(
			ctx, projectsDirectory, projectPath, ".project-update-backup-*", projects.ProjectUpdateBackupScope{Paths: absentPaths},
		)
		if backupProjectDirectoryErr != nil {
			return nil, backupProjectDirectoryErr
		}
		defer cleanupBackup()
		undo = func() error {
			return projects.RestoreProjectDirectoryBackup(cleanupCtx, projectsDirectory, projectPath, backup)
		}
	}
	rollback := func(cause error) error {
		if undoErr := undo(); undoErr != nil {
			return errors.Join(cause, fmt.Errorf("roll back project directory: %w", undoErr))
		}
		return cause
	}

	if applyProjectWorkspaceChangesErr := projects.ApplyProjectWorkspaceChanges(ctx, projectPath, manifest.FileChanges, uploads, projects.ProjectWorkspaceApplyOptions{
		MaxDepth:         s.config.ProjectWorkspaceMaxDepth,
		MaxEntries:       s.config.ProjectWorkspaceMaxEntries,
		MaxFileSizeBytes: workspacepkg.MaxFileSizeBytes(s.config.ProjectWorkspaceMaxFileSizeMB),
		SkipDirectories:  s.config.ProjectScanSkipDirs,
		ComposeFileName:  projects.DefaultComposeFileName,
	}); applyProjectWorkspaceChangesErr != nil {
		return nil, rollback(workspace.WrapProjectWorkspaceError(applyProjectWorkspaceChangesErr))
	}

	// GitOps-originated creates (allowNameSuffix=false) tolerate not-yet-supplied
	// ${VAR} references the same way single-file git sync updates do; interactive
	// creates (allowNameSuffix=true) stay strict.
	if validateComposeContentForUpdateErr := projects.ValidateComposeContentForUpdate(
		ctx,
		projectsDirectory,
		projectPath,
		name,
		composeContent,
		envContent,
		nil,
		"",
		!allowNameSuffix,
	); validateComposeContentForUpdateErr != nil {
		return nil, rollback(fmt.Errorf("invalid compose file: %w", validateComposeContentForUpdateErr))
	}

	if writeProjectFilesErr := projects.WriteProjectFiles(ctx, projectsDirectory, projectPath, composeContent, envContent); writeProjectFilesErr != nil {
		return nil, rollback(fmt.Errorf("failed to save project files: %w", writeProjectFilesErr))
	}
	composeMeta, err := projects.ParseArcaneComposeMetadata(
		ctx,
		filepath.Join(projectPath, projects.DefaultComposeFileName),
		projectsDirectory,
		s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
	)
	if err != nil {
		slog.WarnContext(ctx, "failed to read Compose project tags during creation", "projectName", name, "error", err)
	}
	// Compose owns the tags it declares, so they are not attached as UI tags.
	normalizedUITags = slices.DeleteFunc(normalizedUITags, func(tag string) bool {
		return slices.ContainsFunc(kit.Ternary(err == nil, composeMeta.ProjectTags, nil), func(composeTag projecttypes.TagOption) bool { return composeTag.Name == tag })
	})

	proj := &Project{
		Name:    name,
		DirName: &folderName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}

	if transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if createProjectErr := tx.Create(proj).Error; createProjectErr != nil {
			return createProjectErr
		}
		return tags.AttachInitial(tagStore{tx: tx}, proj.ID, normalizedUITags, normalizedTagColors)
	}); transactionErr != nil {
		return nil, rollback(fmt.Errorf("failed to create project: %w", transactionErr))
	}
	s.refreshComposeProjectName(ctx, proj)
	s.refreshProjectImageRefs(ctx, proj)
	if reconcileComposeProjectTagsErr := s.reconcileComposeProjectTags(ctx, proj.ID, kit.Ternary(err == nil, composeMeta.ProjectTags, nil)); reconcileComposeProjectTagsErr != nil {
		databaseCleanupErr := s.db.WithContext(context.WithoutCancel(ctx)).Transaction(func(tx *gorm.DB) error {
			return deleteProjectWithTags(tx, proj.ID)
		})
		return nil, rollback(errors.Join(
			fmt.Errorf("reconcile Compose project tags: %w", reconcileComposeProjectTagsErr),
			kit.Ternary(databaseCleanupErr == nil, nil, fmt.Errorf("rollback project database state after tag reconciliation failure: %w", databaseCleanupErr)),
		))
	}

	s.logProjectEvent(
		ctx, event.EventTypeProjectCreate, proj.ID, proj.Name, user,
		database.JSON{"action": "create", "projectID": proj.ID, "projectName": proj.Name, "path": projectPath, "reusedDirectory": reused},
		"could not log project creation",
	)

	return proj, nil
}

func (s *ProjectService) DestroyProject(ctx context.Context, projectID string, removeFiles, removeVolumes bool, user usertypes.Actor) error {
	slog.DebugContext(ctx, "DestroyProject service called",
		"projectId", projectID,
		"removeFiles", removeFiles,
		"removeVolumes", removeVolumes,
		"userId", user.ID,
		"username", user.Username)

	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "Found project to destroy",
		"projectName", proj.Name,
		"projectPath", proj.Path)

	if downProjectErr := s.DownProject(ctx, projectID, usertypes.SystemUser); downProjectErr != nil {
		slog.WarnContext(ctx, "failed to bring down project", "error", downProjectErr)
	}

	if removeVolumes {
		if compProj, _, lerr := s.loadComposeProjectForProject(ctx, proj, nil); lerr == nil {
			defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()
			if derr := projects.ComposeDown(ctx, compProj, true); derr != nil {
				slog.WarnContext(ctx, "failed to remove volumes", "error", derr)
			}
		} else {
			slog.WarnContext(ctx, "failed to load compose project for volume removal", "error", lerr)
		}
	}

	if removeFiles {
		slog.DebugContext(ctx, "Removing project files", "path", proj.Path)
		// An imported project can live anywhere, so the removal is rooted at the
		// parent directory and names the project directory itself.
		if removeAllErr := acfs.RemoveAll(ctx, filepath.Dir(proj.Path), "/"+filepath.Base(proj.Path)); removeAllErr != nil {
			slog.ErrorContext(ctx, "Failed to remove project files", "path", proj.Path, "error", removeAllErr)
			return fmt.Errorf("failed to remove project files: %w", removeAllErr)
		}
		slog.InfoContext(ctx, "Project files removed successfully", "path", proj.Path)
	}

	if transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deleteProjectWithTags(tx, projectID)
	}); transactionErr != nil {
		return fmt.Errorf("failed to delete project from database: %w", transactionErr)
	}

	if !removeFiles {
		if projectsDir, dirErr := s.GetProjectsDirectory(ctx); dirErr != nil {
			slog.WarnContext(ctx, "Failed to resolve projects directory for quarantine", "error", dirErr)
		} else if projects.IsSafeSubdirectory(projectsDir, proj.Path) && filepath.Clean(projectsDir) != filepath.Clean(proj.Path) {
			trashName := fmt.Sprintf("%s%s-%d", projects.ArcaneTrashPrefix, filepath.Base(proj.Path), time.Now().Unix())
			trashPath := filepath.Join(filepath.Dir(proj.Path), trashName)
			if renameErr := acfs.Rename(ctx, filepath.Dir(proj.Path), "/"+filepath.Base(proj.Path), "/"+trashName); renameErr != nil {
				slog.WarnContext(ctx, "Failed to quarantine project files", "path", proj.Path, "trashPath", trashPath, "error", renameErr)
			} else {
				slog.InfoContext(ctx, "Project files quarantined successfully", "path", proj.Path, "trashPath", trashPath)
			}
		}
	}
	s.invalidateProjectCaches(projectID)

	s.logProjectEvent(ctx, event.EventTypeProjectDelete, projectID, proj.Name, user, database.JSON{
		"action":        "destroy",
		"projectID":     projectID,
		"projectName":   proj.Name,
		"removeFiles":   removeFiles,
		"removeVolumes": removeVolumes,
	}, "could not log project destroy action")

	return nil
}

func (s *ProjectService) RedeployProject(ctx context.Context, projectID string, user usertypes.Actor, options *projecttypes.DeployOptions) error {
	proj, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return err
	}

	if _, resolveProjectComposeFileErr := s.ResolveProjectComposeFile(ctx, proj); resolveProjectComposeFileErr != nil {
		return resolveProjectComposeFileErr
	}

	// Arcane's own server container must go through the system upgrade flow. A
	// failed container listing skips the guard rather than blocking the redeploy.
	if containers, listErr := s.details.ComposeContainers(ctx); listErr != nil {
		slog.WarnContext(ctx, "could not list compose containers to check self-redeploy guard; skipping guard", "error", listErr)
	} else {
		currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
		projectContainers := listing.ProjectContainers(projectRecord(*proj), listing.GroupComposeContainersByProject(containers))
		if slices.ContainsFunc(projectContainers, func(c container.Summary) bool {
			return labels.ShouldDisableArcaneServerRedeploy(c.Labels, c.ID, currentContainerID, currentContainerErr)
		}) {
			return errors.New("arcane cannot redeploy itself; use the system upgrade flow (Settings -> Updates) instead")
		}
	}

	progressWriter, _ := ctx.Value(dockertypes.ProgressWriterKey{}).(io.Writer)
	credentials, cerr := s.ResolveRegistryCredentials(ctx)
	if cerr != nil {
		slog.WarnContext(ctx, "failed to resolve registry credentials for redeploy pull", "error", cerr)
	}
	if pullProjectImagesErr := s.PullProjectImages(ctx, projectID, kit.Ternary(progressWriter == nil, io.Discard, progressWriter), user, credentials); pullProjectImagesErr != nil {
		slog.WarnContext(ctx, "failed to pull project images", "error", pullProjectImagesErr)
	}

	return s.DeployProject(ctx, projectID, user, options)
}

func (s *ProjectService) PullProjectImages(ctx context.Context, projectID string, progressWriter io.Writer, user usertypes.Actor, credentials []containerregistry.Credential) error {
	proj, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return err
	}

	compProj, _, lerr := s.loadComposeProjectForProject(ctx, proj, nil)
	if lerr != nil {
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	for _, imageRef := range projects.PullableImageRefs(compProj) {
		if pullErr := s.deployment.Pull(ctx, imageRef, progressWriter, user, credentials); pullErr != nil {
			return pullErr
		}
	}
	return nil
}

func (s *ProjectService) BuildProjectServices(ctx context.Context, projectID string, options projecttypes.BuildOptions, progressWriter io.Writer, user *usertypes.Actor) error {
	projectFromDb, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return err
	}

	projectModel, _, derr := s.loadComposeProjectForProject(ctx, projectFromDb, nil)
	if derr != nil {
		return fmt.Errorf("failed to load compose project in %s: %w", projectFromDb.Path, derr)
	}

	defer s.eventService.BeginComposeSuppressionWindow(projectModel.Name)()

	return s.composeCoordinator.BuildServices(ctx, projectID, projectModel, options, progressWriter, s.deployment.ImageOperations(user, nil))
}

func (s *ProjectService) RestartProject(ctx context.Context, projectID string, services []string, user usertypes.Actor) error {
	proj, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return err
	}
	if _, resolveProjectComposeFileErr := s.ResolveProjectComposeFile(ctx, proj); resolveProjectComposeFileErr != nil {
		return resolveProjectComposeFileErr
	}

	if updateProjectStatusErr := s.updateProjectStatus(ctx, projectID, ProjectStatusRestarting); updateProjectStatusErr != nil {
		return fmt.Errorf("failed to update project status to restarting: %w", updateProjectStatusErr)
	}

	compProj, _, lerr := s.loadComposeProjectForProject(ctx, proj, nil)
	if lerr != nil {
		_ = s.updateProjectStatus(ctx, projectID, ProjectStatusRunning)
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	if composeRestartErr := projects.ComposeRestart(ctx, compProj, services); composeRestartErr != nil {
		_ = s.updateProjectStatus(ctx, projectID, ProjectStatusRunning)
		return fmt.Errorf("failed to restart project: %w", composeRestartErr)
	}

	metadata := database.JSON{
		"action":      "restart",
		"projectID":   projectID,
		"projectName": proj.Name,
	}
	if len(services) > 0 {
		metadata["services"] = append([]string(nil), services...)
	}
	s.logProjectEvent(ctx, event.EventTypeProjectStart, projectID, proj.Name, user, metadata, "could not log project restart action")

	return s.updateProjectStatusAndCounts(ctx, projectID, ProjectStatusRunning)
}

func (s *ProjectService) updateProjectStatusAndCounts(ctx context.Context, projectID string, status ProjectStatus) error {
	services, err := s.projectServices(ctx, projectID)
	if err != nil {
		slog.ErrorContext(ctx, "loading project services failed during status update", "projectId", projectID, "error", err)
		return s.updateProjectStatus(ctx, projectID, status)
	}

	serviceCount, runningCount := listing.ServiceCounts(services)
	if updateStatusErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"status":        status,
		"service_count": serviceCount,
		"running_count": runningCount,
		"updated_at":    time.Now(),
	}).Error; updateStatusErr != nil {
		return fmt.Errorf("failed to update project status and counts: %w", updateStatusErr)
	}
	return nil
}

func (s *ProjectService) updateProjectStatus(ctx context.Context, id string, status ProjectStatus) error {
	if err := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", id).Updates(map[string]any{
		"status":     status,
		"updated_at": time.Now(),
	}).Error; err != nil {
		return fmt.Errorf("failed to update project status: %w", err)
	}
	return nil
}

func (s *ProjectService) projectContent(ctx context.Context, projectID string) (composeContent, envContent, overrideContent string, err error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return "", "", "", err
	}

	composePath, composeErr := s.ResolveProjectComposeFile(ctx, proj)
	switch {
	case composeErr == nil, errors.Is(composeErr, common.ErrProjectComposeFileNotFound):
	case errors.Is(composeErr, common.ErrProjectEnvUnreadable):
		projectsDirectory, dirErr := s.GetProjectsDirectory(ctx)
		if dirErr != nil {
			slog.DebugContext(ctx, "failed to resolve projects directory for compose identification", "projectId", proj.ID, "error", dirErr)
		}
		composePath, composeErr = projects.DetectComposeFile(ctx, projectsDirectory, proj.Path)
		if composeErr != nil && (!errors.Is(composeErr, common.ErrProjectEnvUnreadable) || composePath == "") {
			return "", "", "", fmt.Errorf("failed to identify project compose file: %w", composeErr)
		}
	default:
		return "", "", "", composeErr
	}

	composeContent, envContent, err = projects.ReadProjectFiles(ctx, proj.Path, composePath)
	if err != nil {
		return "", "", "", err
	}

	return composeContent, envContent, projects.ReadComposeOverrideContent(proj.Path), nil
}

func (s *ProjectService) GetProjectDetails(ctx context.Context, projectID string, opts projecttypes.DetailsOptions) (projecttypes.Details, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return projecttypes.Details{}, err
	}
	projectsDir, projectsDirErr := s.GetProjectsDirectory(ctx)
	if projectsDirErr != nil {
		// Relative paths and the .env.global selection layer degrade without a projects directory.
		slog.WarnContext(ctx, "failed to resolve projects directory for project details", "projectId", projectID, "error", projectsDirErr)
	}
	projectTags, err := s.GetProjectTags(ctx, projectID)
	if err != nil {
		return projecttypes.Details{}, err
	}

	// COMPOSE_FILE selects the deployed file set (no auto-overrides); a broken selection is logged only.
	composeSelection, selErr := projects.ComposeFileEnvSelection(ctx, projectsDir, proj.Path)
	if selErr != nil {
		slog.Log(ctx, kit.Ternary(errors.Is(selErr, common.ErrProjectEnvUnreadable), slog.LevelDebug, slog.LevelWarn),
			"failed to resolve COMPOSE_FILE selection for project details", "projectId", proj.ID, "path", proj.Path, "error", selErr)
	}
	composeContent, _, overrideContent, err := kit.Ternary(opts.IncludeComposeContent, s.projectContent, func(context.Context, string) (string, string, string, error) {
		return "", "", "", nil
	})(ctx, proj.ID)
	if err != nil {
		return projecttypes.Details{}, fmt.Errorf("failed to read project compose content: %w", err)
	}
	overrideFileName, overrideContent := func() (string, string) {
		if !opts.IncludeComposeContent {
			return "", ""
		}
		return projectdetails.ResolveDetailsOverride(proj.Path, overrideContent, kit.Ternary(selErr == nil, composeSelection, nil))
	}()
	envContent, err := func() (string, error) {
		if !opts.IncludeEnvState {
			return "", nil
		}
		envState, readErr := projects.ReadProjectEnvState(proj.Path)
		if readErr != nil {
			return "", fmt.Errorf("failed to read project env state: %w", readErr)
		}
		return projectsync.ResolveStoredEffectiveEnvContent(envState)
	}()
	if err != nil {
		return projecttypes.Details{}, err
	}

	composeFile, composeErr := s.ResolveProjectComposeFile(ctx, proj)
	if errors.Is(composeErr, common.ErrProjectEnvUnreadable) {
		// The env is unreadable, so only the compose file's identity is known; interpolation-backed enrichment is skipped.
		projectsDirectory, dirErr := s.GetProjectsDirectory(ctx)
		if dirErr != nil {
			slog.DebugContext(ctx, "failed to resolve projects directory for compose identification", "projectId", proj.ID, "error", dirErr)
		}
		identified, detectErr := projects.DetectComposeFile(ctx, projectsDirectory, proj.Path)
		if detectErr != nil && (!errors.Is(detectErr, common.ErrProjectEnvUnreadable) || identified == "") {
			slog.WarnContext(ctx, "failed to identify project compose file", "projectId", proj.ID, "error", detectErr)
		}
		composeFile = kit.Ternary(detectErr != nil && (!errors.Is(detectErr, common.ErrProjectEnvUnreadable) || identified == ""), "", identified)
	}
	composeServices := func() []types.ServiceConfig {
		if composeErr != nil || (!opts.IncludeServiceConfigs && !opts.IncludeUpdateInfo) {
			return nil
		}
		composeProj, loadErr := s.getCachedComposeProject(ctx, proj, nil)
		if loadErr != nil {
			slog.WarnContext(ctx, "failed to load compose service configs", "path", composeFile, "error", loadErr)
			return nil
		}
		return slices.Collect(maps.Values(composeProj.Services))
	}()

	var syncRecord GitOpsSync
	if gitOpsSyncID(proj) != "" {
		_ = s.db.WithContext(ctx).Preload("Repository").Where("id = ?", gitOpsSyncID(proj)).First(&syncRecord).Error
	}

	// Runtime status/counts refresh even without the full runtime service array; DB values are only a fallback.
	services, serr := s.projectServices(ctx, projectID)
	_, runningCount := listing.ServiceCounts(services)
	runtimeServices := kit.Ternary(serr == nil && services != nil && (opts.IncludeRuntimeServices || opts.IncludeUpdateInfo), services, nil)
	meta := s.ProjectMetadata(ctx, *proj, nil)
	icon := iconcatalog.Resolve(IconCatalogForContext(ctx), meta.ProjectIcon)
	return projecttypes.Details{
		Tags:            projectTags,
		StatusReason:    proj.StatusReason,
		LastSyncCommit:  syncRecord.LastSyncCommit,
		GitOpsManagedBy: proj.GitOpsManagedBy,
		UpdateInfo: func() *projecttypes.UpdateInfo {
			if !opts.IncludeUpdateInfo {
				return nil
			}
			imageRefs := projects.ImageRefsFromComposeConfigs(composeServices)
			if len(imageRefs) == 0 {
				imageRefs = projects.ImageRefsFromRuntimeServices(runtimeServices)
			}
			updateInfoByRef, updateInfoErr := s.imageService.GetUpdateInfoByImageRefs(ctx, imageRefs)
			if updateInfoErr != nil {
				slog.WarnContext(ctx, "failed to fetch project update info", "projectId", proj.ID, "projectName", proj.Name, "error", updateInfoErr)
			}
			scoped := s.details.ContainerUpdateInfo(ctx, runtimeServices)
			if len(composeServices) > 0 {
				return BuildConfiguredUpdateInfo(
					proj.ID,
					composeServices,
					updateInfoByRef,
					s.details.ServiceUpdateRecords(ctx, []string{proj.ID}),
					projectdetails.ConfiguredRuntimeServiceUpdateInfo(composeServices, runtimeServices, scoped),
				)
			}
			return buildUpdateInfoSummary(imageRefs, projectdetails.MergeProjectContainerUpdateInfo(updateInfoByRef, runtimeServices, scoped), nil)
		}(),
		ArchivedAt:       proj.ArchivedAt,
		Status:           kit.Ternary(serr == nil && services != nil, listing.ProjectStatus(services), string(proj.Status)),
		DirName:          kit.FromPtr(proj.DirName),
		ComposeContent:   composeContent,
		ComposeFileName:  kit.Ternary(composeFile != "", filepath.Base(composeFile), ""),
		ComposeFiles:     projectdetails.ComposeSelectionRelativePaths(proj.Path, kit.Ternary(selErr == nil, composeSelection, nil)),
		EnvContent:       envContent,
		OverrideContent:  overrideContent,
		OverrideFileName: overrideFileName,
		Name:             proj.Name,
		GitRepositoryURL: kit.FromPtr(syncRecord.Repository).URL,
		IconLightURL:     icon.IconLightURL,
		RelativePath:     listing.RelativePath(projectsDir, proj.Path),
		ID:               proj.ID,
		IconDarkURL:      icon.IconDarkURL,
		Path:             proj.Path,
		UpdatedAt:        proj.UpdatedAt.Format(time.RFC3339),
		CreatedAt:        proj.CreatedAt.Format(time.RFC3339),
		RuntimeServices:  kit.Ternary(opts.IncludeRuntimeServices, runtimeServices, nil),
		Services:         kit.Ternary(opts.IncludeServiceConfigs, composeServices, nil),
		URLs:             meta.ProjectURLS,
		IncludeFiles: func() []projecttypes.IncludeFile {
			if composeErr != nil || !opts.IncludeIncludeFiles {
				return nil
			}
			return s.details.IncludeFiles(ctx, composeFile)
		}(),
		RunningCount: kit.Ternary(serr == nil && services != nil, runningCount, proj.RunningCount),
		ServiceCount: kit.Ternary(serr == nil && services != nil, len(services), proj.ServiceCount),
		IsArchived:   proj.IsArchived,
		HasBuildDirective: len(projects.ParseImageRefsJSON(kit.FromPtr(proj.BuildImageRefsJSON))) > 0 ||
			slices.ContainsFunc(composeServices, func(svc types.ServiceConfig) bool { return svc.Build != nil }),
		RedeployDisabled:   slices.ContainsFunc(runtimeServices, func(svc projecttypes.RuntimeService) bool { return svc.RedeployDisabled }),
		ConfigurationError: projects.CheckProjectEnvAccess(ctx, projectsDir, proj.Path),
	}, nil
}

func (s *ProjectService) enrichProjectsWithUpdateInfo(
	ctx context.Context,
	projectsList []Project,
	details []projecttypes.Details,
	includeHidden bool,
	env *projectMetadataEnv,
) []projecttypes.Details {
	if len(projectsList) == 0 || len(details) == 0 {
		return details
	}
	if env == nil {
		env = s.newProjectMetadataEnv(ctx, projectsList)
	}

	runtimeByIndex, hiddenServicesByProjectID, hiddenRefsByProjectID := func() ([][]projecttypes.RuntimeService, map[string]map[string]bool, map[string]map[string]bool) {
		if includeHidden {
			runtime := make([][]projecttypes.RuntimeService, len(details))
			for i, d := range details {
				runtime[i] = d.RuntimeServices
			}
			return runtime, nil, nil
		}
		return projectdetails.ExcludeHiddenRuntimeServices(details)
	}()

	// Bounded fan-out over compose loads; a cancelled request leaves the
	// remaining projects without configured services.
	refsByIndex := make([][]string, len(projectsList))
	servicesByIndex := make([][]types.ServiceConfig, len(projectsList))
	var g errgroup.Group
	g.SetLimit(maxConcurrentComposeReads)
	for i, proj := range projectsList {
		g.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			composeProject, err := s.getCachedComposeProject(ctx, &proj, env)
			if err != nil {
				slog.WarnContext(ctx, "failed to resolve project services for update summary", "projectId", proj.ID, "projectName", proj.Name, "error", err)
				refsByIndex[i] = slices.DeleteFunc(projects.ParseImageRefsJSON(proj.ImageRefsJSON), func(ref string) bool { return hiddenRefsByProjectID[proj.ID][ref] })
				return nil
			}
			services := make([]types.ServiceConfig, 0, len(composeProject.Services))
			for _, service := range composeProject.Services {
				hidden, _ := kit.ParseBool(service.Labels[libarcane.HiddenResourceLabel])
				if includeHidden || (!hidden && !hiddenServicesByProjectID[proj.ID][service.Name]) {
					services = append(services, service)
				}
			}
			refsByIndex[i], servicesByIndex[i] = projects.ImageRefsFromComposeConfigs(services), services
			return nil
		})
	}
	_ = g.Wait()

	projectIDs := make([]string, len(projectsList))
	imageRefsByProjectID := make(map[string][]string, len(projectsList))
	servicesByProjectID := make(map[string][]types.ServiceConfig, len(projectsList))
	for i, proj := range projectsList {
		projectIDs[i] = proj.ID
		imageRefsByProjectID[proj.ID], servicesByProjectID[proj.ID] = refsByIndex[i], servicesByIndex[i]
	}

	updateInfoByRef, err := s.imageService.GetUpdateInfoByImageRefs(ctx, slices.Concat(refsByIndex...))
	if err != nil {
		slog.WarnContext(ctx, "failed to fetch project list update info", "error", err)
	}

	recordsByProjectID := make(map[string][]imageupdate.ImageUpdateRecord)
	for _, record := range s.details.ServiceUpdateRecords(ctx, projectIDs) {
		recordsByProjectID[record.ProjectID] = append(recordsByProjectID[record.ProjectID], record)
	}
	scoped := s.details.ContainerUpdateInfo(ctx, slices.Concat(runtimeByIndex...))
	out := make([]projecttypes.Details, 0, len(details))
	for i, d := range details {
		out = append(out, projecttypes.Details{ //exhaustruct:enforce
			EnvironmentTag:  d.EnvironmentTag,
			Tags:            d.Tags,
			StatusReason:    d.StatusReason,
			ActivityID:      d.ActivityID,
			LastSyncCommit:  d.LastSyncCommit,
			GitOpsManagedBy: d.GitOpsManagedBy,
			UpdateInfo: func() *projecttypes.UpdateInfo {
				if services := servicesByProjectID[d.ID]; services != nil {
					return BuildConfiguredUpdateInfo(
						d.ID,
						services,
						updateInfoByRef,
						recordsByProjectID[d.ID],
						projectdetails.ConfiguredRuntimeServiceUpdateInfo(services, runtimeByIndex[i], scoped),
					)
				}
				refs := imageRefsByProjectID[d.ID]
				if len(refs) == 0 {
					refs = projects.ImageRefsFromRuntimeServices(runtimeByIndex[i])
				}
				return buildUpdateInfoSummary(refs, projectdetails.MergeProjectContainerUpdateInfo(updateInfoByRef, runtimeByIndex[i], scoped), nil)
			}(),
			ArchivedAt:         d.ArchivedAt,
			Status:             d.Status,
			DirName:            d.DirName,
			ComposeContent:     d.ComposeContent,
			ComposeFileName:    d.ComposeFileName,
			ComposeFiles:       d.ComposeFiles,
			EnvContent:         d.EnvContent,
			OverrideContent:    d.OverrideContent,
			OverrideFileName:   d.OverrideFileName,
			Name:               d.Name,
			GitRepositoryURL:   d.GitRepositoryURL,
			IconLightURL:       d.IconLightURL,
			RelativePath:       d.RelativePath,
			ID:                 d.ID,
			IconDarkURL:        d.IconDarkURL,
			Path:               d.Path,
			UpdatedAt:          d.UpdatedAt,
			CreatedAt:          d.CreatedAt,
			RuntimeServices:    runtimeByIndex[i],
			Services:           d.Services,
			URLs:               d.URLs,
			IncludeFiles:       d.IncludeFiles,
			RunningCount:       d.RunningCount,
			ServiceCount:       d.ServiceCount,
			IsDiscovered:       d.IsDiscovered,
			IsArchived:         d.IsArchived,
			HasBuildDirective:  d.HasBuildDirective,
			RedeployDisabled:   d.RedeployDisabled,
			ConfigurationError: d.ConfigurationError,
		})
	}
	return out
}

// BuildConfiguredUpdateInfo matches checks to current services and aggregates their results.
func BuildConfiguredUpdateInfo(
	projectID string,
	services []types.ServiceConfig,
	byRef map[string]*imagetypes.UpdateInfo,
	records []imageupdate.ImageUpdateRecord,
	runtimeUpdates ...map[string]*imagetypes.UpdateInfo,
) *projecttypes.UpdateInfo {
	checks := make(map[string]*imageupdate.ImageUpdateRecord)
	for i := range records {
		if records[i].ProjectID == projectID {
			checks[records[i].ServiceName] = &records[i]
		}
	}
	serviceUpdates := make(map[string]projecttypes.ServiceUpdateInfo, len(services))
	selected := make(map[string]*imagetypes.UpdateInfo)
	runtime := make([]projecttypes.RuntimeService, 0, len(services))
	unknownRefs := make(map[string]bool)
	for _, service := range services {
		imageRef := strings.TrimSpace(service.Image)
		if imageRef == "" {
			continue
		}
		record := checks[service.Name]
		policy, policyErr := tagpolicy.Resolve(imageRef, updater.DefaultLabelPolicy().TagPolicy(service.Labels))
		info := func() *imagetypes.UpdateInfo {
			switch {
			case record != nil && record.PolicyKey == imageref.UpdatePolicyKey(imageRef, service.Labels):
				return record.UpdateInfo()
			case policyErr == nil && policy.Strategy == "digest" && !imageref.IsUpdateCheckDisabled(service.Labels):
				return byRef[imageRef]
			default:
				return nil
			}
		}()

		if len(runtimeUpdates) > 0 && runtimeUpdates[0][service.Name] != nil {
			// A newer clean runtime check clears a stale update preview (#4306);
			// digest checks must match the configured reference to count.
			runtime := runtimeUpdates[0][service.Name]
			stored := byRef[imageRef]
			attributed := policyErr == nil && (policy.Strategy != "digest" || (stored != nil && !stored.HasUpdate && stored.Error == "" &&
				stored.CheckTime.Equal(runtime.CheckTime) && stored.CurrentDigest == runtime.CurrentDigest && stored.LatestDigest == runtime.LatestDigest))
			info = projectdetails.MergeProjectContainerUpdateInfo(
				nil,
				[]projecttypes.RuntimeService{
					{
						ContainerID: "preview",
						Image:       imageRef,
					},
					{
						ContainerID: "runtime",
						Image:       imageRef,
					},
				},
				map[string]*imagetypes.UpdateInfo{
					"preview": kit.Ternary(attributed && info != nil && info.HasUpdate && !runtime.HasUpdate && runtime.Error == "" && !runtime.CheckTime.Before(info.CheckTime), nil, info),
					"runtime": runtime,
				},
			)[imageRef]
		}
		serviceUpdates[service.Name] = projecttypes.ServiceUpdateInfo{ImageRef: imageRef, UpdateInfo: info}
		selected[service.Name] = info
		runtime = append(runtime, projecttypes.RuntimeService{ContainerID: service.Name, Image: imageRef})
		if info == nil {
			unknownRefs[imageRef] = true
		}
	}
	merged := projectdetails.MergeProjectContainerUpdateInfo(nil, runtime, selected)
	for imageRef := range unknownRefs {
		if info := merged[imageRef]; info == nil || !info.HasUpdate {
			delete(merged, imageRef)
		}
	}
	return buildUpdateInfoSummary(projects.ImageRefsFromComposeConfigs(services), merged, serviceUpdates)
}

// buildUpdateInfoSummary aggregates image checks into a project update summary.
func buildUpdateInfoSummary(
	imageRefs []string,
	updateInfoByRef map[string]*imagetypes.UpdateInfo,
	serviceUpdates map[string]projecttypes.ServiceUpdateInfo,
) *projecttypes.UpdateInfo {
	checkedByRef := make(map[string]imagetypes.UpdateInfo)
	var updatedRefs, notPulledRefs []string
	var errorMessages []string
	var latestCheckTime *time.Time
	for _, imageRef := range imageRefs {
		info := updateInfoByRef[imageRef]
		if info == nil {
			continue
		}
		checkedByRef[imageRef] = *info
		if info.HasUpdate {
			updatedRefs = append(updatedRefs, imageRef)
		}
		if info.UpdateType == imageupdate.UpdateTypeNotPulled {
			notPulledRefs = append(notPulledRefs, imageRef)
		}
		if strings.TrimSpace(info.Error) != "" {
			errorMessages = append(errorMessages, strings.TrimSpace(info.Error))
		}
		if !info.CheckTime.IsZero() && (latestCheckTime == nil || info.CheckTime.After(*latestCheckTime)) {
			latestCheckTime = new(info.CheckTime)
		}
	}

	return &projecttypes.UpdateInfo{
		ServiceUpdates: serviceUpdates,
		Status: func() string {
			switch {
			case len(imageRefs) == 0:
				return "unknown"
			case len(updatedRefs) > 0:
				return "has_update"
			case len(errorMessages) > 0:
				return "error"
			case len(notPulledRefs) > 0:
				return "not_pulled"
			case len(checkedByRef) == len(imageRefs):
				return "up_to_date"
			default:
				return "unknown"
			}
		}(),
		HasUpdate:         len(updatedRefs) > 0,
		ImageCount:        len(imageRefs),
		CheckedImageCount: len(checkedByRef),
		ImagesWithUpdates: len(updatedRefs),
		ImagesNotPulled:   len(notPulledRefs),
		ErrorCount:        len(errorMessages),
		ErrorMessage: func() *string {
			if len(errorMessages) == 0 {
				return nil
			}
			return new(errorMessages[0])
		}(),
		ImageRefs:          append([]string(nil), imageRefs...),
		UpdatedImageRefs:   updatedRefs,
		NotPulledImageRefs: notPulledRefs,
		UpdateInfoByRef:    kit.Ternary(len(checkedByRef) > 0, checkedByRef, nil),
		LastCheckedAt:      latestCheckTime,
	}
}

func (s *ProjectService) CountServicesFromCompose(ctx context.Context, p Project) (int, error) {
	proj, _, err := s.loadComposeProjectForProject(ctx, &p, nil)
	if err != nil {
		return 0, err
	}

	return len(proj.Services), nil
}

func (s *ProjectService) ListAllProjects(ctx context.Context) ([]Project, error) {
	var items []Project
	if err := s.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return items, nil
}

func (s *ProjectService) ListProjects(ctx context.Context, params pagination.QueryParams) ([]projecttypes.Details, pagination.Response, error) {
	updatesFilter := strings.TrimSpace(params.Filters["updates"])
	query := s.db.WithContext(ctx).Model(&Project{})
	if archivedFilter := strings.TrimSpace(params.Filters["archived"]); !strings.EqualFold(archivedFilter, "all") {
		archived, _ := kit.ParseBool(archivedFilter)
		query = query.Where("is_archived = ?", archived)
	}
	query = listing.ApplyProjectTagsDBFilter(query, strings.TrimSpace(params.Filters["tags"]))
	query = pagination.ApplyFilter(query, "name", strings.TrimSpace(params.Filters["name"]))
	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where(
			"name LIKE ? OR path LIKE ? OR status LIKE ? OR COALESCE(dir_name, '') LIKE ? OR "+
				"EXISTS (SELECT 1 FROM project_tags WHERE project_tags.project_id = projects.id AND LOWER(project_tags.name) LIKE ?)",
			pattern, pattern, pattern, pattern, strings.ToLower(pattern),
		)
	}

	// Derived filters and status sorting read the container snapshot, so they page in memory over every candidate;
	// otherwise the database pages and only the page gets live status and compose-backed presentation fields.
	paginateInDB := strings.TrimSpace(params.Filters["status"]) == "" && updatesFilter == "" && strings.TrimSpace(params.Filters["label"]) == "" &&
		!strings.EqualFold(strings.TrimSpace(params.Sort), "status")
	if !paginateInDB && params.Limit != -1 {
		params.Limit = kit.Ternary(params.Limit <= 0, 20, min(params.Limit, 100))
	}
	var projectsArray []Project
	paginationResp, err := func() (pagination.Response, error) {
		if !paginateInDB {
			if findErr := query.Find(&projectsArray).Error; findErr != nil {
				return pagination.Response{}, fmt.Errorf("failed to list projects: %w", findErr)
			}
			return pagination.Response{}, nil
		}
		resp, paginateErr := pagination.PaginateAndSortDB(params, pagination.ApplyFilter(query, "status", params.Filters["status"]), &projectsArray)
		if paginateErr != nil {
			return pagination.Response{}, fmt.Errorf("failed to paginate projects: %w", paginateErr)
		}
		return resp, nil
	}()
	if err != nil {
		return nil, pagination.Response{}, err
	}

	byID := make(map[string]Project, len(projectsArray))
	for _, proj := range projectsArray {
		byID[proj.ID] = proj
	}
	var tagRows []projecttypes.TagAssignment
	if tagsErr := s.db.WithContext(ctx).Model(&ProjectTag{}).Where("project_id IN ?", slices.Collect(maps.Keys(byID))).Order("project_id, name, source, color").Find(&tagRows).Error; tagsErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("load project tags: %w", tagsErr)
	}
	env := s.newProjectMetadataEnv(ctx, nil)
	snapshot := s.listing.Snapshot(ctx)
	// Live-inferred service counts are persisted over stored zeros, since SQL sorts and paginates on service_count.
	items, inferredCounts := listing.Rows(env.projectsDirectory, IconCatalogForContext(ctx), projectRecords(projectsArray), snapshot, tags.Group(tagRows))
	for chunk := range slices.Chunk(slices.Collect(maps.Keys(inferredCounts)), inferredServiceCountBatchSize) {
		var caseExpr strings.Builder
		args := make([]any, 0, 2*len(chunk))
		caseExpr.WriteString("CASE id")
		for _, id := range chunk {
			caseExpr.WriteString(" WHEN ? THEN ?")
			args = append(args, id, inferredCounts[id])
		}
		caseExpr.WriteString(" ELSE service_count END")
		if updateErr := s.db.WithContext(ctx).Model(&Project{}).
			Where("id IN ? AND service_count = 0", chunk).
			Update("service_count", gorm.Expr(caseExpr.String(), args...)).Error; updateErr != nil {
			slog.WarnContext(ctx, "failed to persist inferred project service counts", "count", len(chunk), "error", updateErr)
			break
		}
	}
	if updatesFilter != "" {
		s.preloadGitOpsComposePaths(ctx, env, projectsArray)
		items = s.enrichProjectsWithUpdateInfo(ctx, projectsArray, items, true, env)
		// Untracked compose projects only join an unfiltered-by-tag has_update view.
		if strings.EqualFold(updatesFilter, "has_update") && strings.TrimSpace(params.Filters["tags"]) == "" {
			if snapshot.Err != nil {
				slog.WarnContext(ctx, "failed to list compose containers for project update rows", "error", snapshot.Err)
			} else {
				// Every tracked project name is known; the candidates cover a failed lookup.
				var knownProjects []Project
				if knownErr := s.db.WithContext(ctx).Select("name", "compose_project_name").Find(&knownProjects).Error; knownErr != nil {
					slog.WarnContext(ctx, "failed to load known project names for compose update discovery", "error", knownErr)
					knownProjects = projectsArray
				}
				items = append(items, s.listing.DiscoveredUpdateRows(ctx, snapshot.Containers, listing.KnownComposeProjectNames(projectRecords(knownProjects)), IconCatalogForContext(ctx))...)
			}
		}
	}

	result, pageIndexes := func() (pagination.FilterResult[projecttypes.Details], []int) {
		if paginateInDB {
			indexes := make([]int, len(items))
			for i := range items {
				indexes[i] = i
			}
			return pagination.FilterResult[projecttypes.Details]{Items: items}, indexes
		}
		return listing.Page(items, params, func(id string) bool {
			_, tracked := byID[id]
			return tracked
		})
	}()
	pageProjects := make([]Project, 0, len(pageIndexes))
	pageDetails := make([]projecttypes.Details, 0, len(pageIndexes))
	for _, i := range pageIndexes {
		pageProjects = append(pageProjects, byID[result.Items[i].ID])
		pageDetails = append(pageDetails, result.Items[i])
	}
	if updatesFilter == "" {
		s.preloadGitOpsComposePaths(ctx, env, pageProjects)
		pageDetails = s.enrichProjectsWithUpdateInfo(ctx, pageProjects, pageDetails, true, env)
	}
	metas := make([]projects.ArcaneComposeMetadata, len(pageProjects))
	var metaGroup errgroup.Group
	metaGroup.SetLimit(maxConcurrentComposeReads)
	for i := range pageProjects {
		metaGroup.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "project metadata worker", "projectID", pageProjects[i].ID)
			metas[i] = s.ProjectMetadata(ctx, pageProjects[i], env)
			return nil
		})
	}
	if metaErr := metaGroup.Wait(); metaErr != nil {
		slog.WarnContext(ctx, "project metadata resolution failed", "error", metaErr)
	}
	for k, d := range listing.ApplyPresentation(ctx, env.projectsDirectory, IconCatalogForContext(ctx), projectRecords(pageProjects), pageDetails, metas) {
		result.Items[pageIndexes[k]] = d
	}
	return result.Items, kit.Ternary(paginateInDB, paginationResp, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)), nil
}

// CountProjectsWithPendingUpdates counts active projects and untracked compose
// projects with a pending update, without the full list pipeline. A nil
// allContainers is fetched here.
func (s *ProjectService) CountProjectsWithPendingUpdates(ctx context.Context, allContainers []container.Summary) (int, error) {
	if allContainers == nil {
		listed, err := s.details.ComposeContainers(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to list containers for project update count: %w", err)
		}
		allContainers = listed
	}

	// One full scan: archived projects are excluded from the update count but
	// still mark their compose stacks as known during discovery, so loading
	// everything here saves the known-name pass its own table scan.
	var allProjects []Project
	if err := s.db.WithContext(ctx).Find(&allProjects).Error; err != nil {
		return 0, fmt.Errorf("failed to list projects for update count: %w", err)
	}

	activeProjects := make([]Project, 0, len(allProjects))
	for _, proj := range allProjects {
		if !proj.IsArchived {
			activeProjects = append(activeProjects, proj)
		}
	}

	// Runtime container IDs keep tag-policy updates scoped to their project.
	details := make([]projecttypes.Details, 0, len(activeProjects))
	containersByProject := listing.GroupComposeContainersByProject(allContainers)
	for _, proj := range activeProjects {
		var runtimeServices []projecttypes.RuntimeService
		for _, c := range listing.ProjectContainers(projectRecord(proj), containersByProject) {
			runtimeServices = append(runtimeServices, projecttypes.RuntimeService{
				Name:            docker.ComposeServiceLabel(c.Labels),
				ContainerID:     c.ID,
				Image:           c.Image,
				ImageID:         c.ImageID,
				ContainerLabels: c.Labels,
			})
		}
		details = append(details, projecttypes.Details{ID: proj.ID, RuntimeServices: runtimeServices})
	}
	count := 0
	for _, d := range s.enrichProjectsWithUpdateInfo(ctx, activeProjects, details, false, nil) {
		if kit.FromPtr(d.UpdateInfo).HasUpdate {
			count++
		}
	}

	visibleContainers := make([]container.Summary, 0, len(allContainers))
	for _, c := range allContainers {
		hidden, _ := kit.ParseBool(c.Labels[libarcane.HiddenResourceLabel])
		if !hidden {
			visibleContainers = append(visibleContainers, c)
		}
	}
	// Untracked compose projects with a pending update count too, so the
	// dashboard badge matches the projects table.
	known := listing.KnownComposeProjectNames(projectRecords(allProjects))
	return count + s.listing.CountDiscoveredUpdates(ctx, visibleContainers, known, IconCatalogForContext(ctx)), nil
}

// ProjectMetadata resolves a project's icon sets and service URLs, cached until
// a merged compose or env file changes. A nil env resolves the shared inputs here.
func (s *ProjectService) ProjectMetadata(ctx context.Context, p Project, env *projectMetadataEnv) projects.ArcaneComposeMetadata {
	empty := projects.ArcaneComposeMetadata{ServiceIconSets: map[string]projects.IconSet{}}

	if env == nil {
		env = s.newProjectMetadataEnv(ctx, []Project{p})
	}
	composeFile, err := s.resolveProjectComposeFile(ctx, &p, env)
	if err != nil {
		return empty
	}

	fingerprint := fmt.Sprintf("%q|%q|%t", composeFile, env.projectsDirectory, env.autoInjectEnv)
	if cached, ok := s.metaCache.Get(p.ID, fingerprint); ok && p.ID != "" {
		return cached
	}

	meta, err := projects.ParseArcaneComposeMetadata(ctx, composeFile, env.projectsDirectory, env.autoInjectEnv)
	if err != nil {
		slog.WarnContext(ctx, "failed to parse Arcane compose metadata", "path", composeFile, "error", err)
		return empty
	}

	if p.ID == "" {
		return meta
	}
	if setErr := s.metaCache.Set(p.ID, fingerprint, p.Path, env.projectsDirectory, composeFile, meta.ComposeFiles, meta.EnvFiles, meta); setErr != nil {
		slog.DebugContext(ctx, "failed to cache Compose metadata", "projectId", p.ID, "error", setErr)
	}

	return meta
}

// IconCatalogForContext resolves the icon catalog of the requesting
// user. On agent-proxied calls the caller is a synthetic user whose preference
// is populated from the X-Arcane-Icon-Catalog header the manager forwards.
// Background jobs have no user attached and fall back to the default catalog.
func IconCatalogForContext(ctx context.Context) string {
	u, _ := userctx.CurrentUserFromContext(ctx)
	return cmp.Or(kit.FromPtr(kit.FromPtr(u).Preferences.IconCatalog), iconcatalog.DefaultCatalog)
}

func (s *ProjectService) refreshProjectImageRefs(ctx context.Context, proj *Project) {
	if proj.ID == "" {
		return
	}

	s.invalidateProjectCaches(proj.ID)
	composeProject, err := s.getCachedComposeProject(ctx, proj, nil)
	if err != nil {
		if dbErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", proj.ID).Update("image_refs_json", "").Error; dbErr != nil {
			slog.WarnContext(ctx, "failed to clear stale project image refs", "projectId", proj.ID, "error", dbErr)
		}
		*proj = Project{ //exhaustruct:enforce
			BaseModel:          proj.BaseModel,
			Name:               proj.Name,
			DirName:            proj.DirName,
			Path:               proj.Path,
			Status:             proj.Status,
			StatusReason:       proj.StatusReason,
			ServiceCount:       proj.ServiceCount,
			RunningCount:       proj.RunningCount,
			GitOpsManagedBy:    proj.GitOpsManagedBy,
			ComposeProjectName: proj.ComposeProjectName,
			ImageRefsJSON:      "",
			BuildImageRefsJSON: proj.BuildImageRefsJSON,
			IsArchived:         proj.IsArchived,
			ArchivedAt:         proj.ArchivedAt,
		}
		slog.WarnContext(ctx, "failed to refresh project image refs", "projectId", proj.ID, "projectName", proj.Name, "error", fmt.Errorf("load compose project: %w", err))
		return
	}
	imageRefsJSON := projects.MarshalImageRefsJSON(projects.ImageRefsFromComposeServices(composeProject.Services))
	buildImageRefsJSON := cmp.Or(projects.MarshalImageRefsJSON(projects.BuildImageRefsFromComposeProject(composeProject)), "[]")
	if persistImageRefsErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", proj.ID).Updates(map[string]any{
		"image_refs_json":       imageRefsJSON,
		"build_image_refs_json": buildImageRefsJSON,
	}).Error; persistImageRefsErr != nil {
		slog.WarnContext(ctx, "failed to persist project image refs", "projectId", proj.ID, "error", persistImageRefsErr)
		return
	}
	*proj = Project{ //exhaustruct:enforce
		BaseModel:          proj.BaseModel,
		Name:               proj.Name,
		DirName:            proj.DirName,
		Path:               proj.Path,
		Status:             proj.Status,
		StatusReason:       proj.StatusReason,
		ServiceCount:       proj.ServiceCount,
		RunningCount:       proj.RunningCount,
		GitOpsManagedBy:    proj.GitOpsManagedBy,
		ComposeProjectName: proj.ComposeProjectName,
		ImageRefsJSON:      imageRefsJSON,
		BuildImageRefsJSON: new(buildImageRefsJSON),
		IsArchived:         proj.IsArchived,
		ArchivedAt:         proj.ArchivedAt,
	}
}

func (s *ProjectService) HandleProjectFilesChanged(ctx context.Context, paths []string) {
	if len(paths) == 0 {
		return
	}

	// Match on paths alone, then load full rows only for the affected projects.
	var candidates []Project
	if err := s.db.WithContext(ctx).Select("id", "path").Find(&candidates).Error; err != nil {
		slog.WarnContext(ctx, "failed to resolve changed project files", "error", fmt.Errorf("list projects for changed paths: %w", err))
		return
	}
	var changedIDs []string
	for _, candidate := range candidates {
		if slices.ContainsFunc(paths, func(changedPath string) bool { return projects.IsSafeSubdirectory(candidate.Path, changedPath) }) {
			changedIDs = append(changedIDs, candidate.ID)
		}
	}
	if len(changedIDs) == 0 {
		return
	}
	var projectsList []Project
	if err := s.db.WithContext(ctx).Where("id IN ?", changedIDs).Find(&projectsList).Error; err != nil {
		slog.WarnContext(ctx, "failed to resolve changed project files", "error", fmt.Errorf("load projects for changed paths: %w", err))
		return
	}
	for i := range projectsList {
		s.refreshProjectImageRefs(ctx, &projectsList[i])
		if err := s.reconcileComposeTagsForProject(ctx, &projectsList[i]); err != nil {
			slog.WarnContext(ctx, "failed to reconcile Compose project tags after file change", "projectId", projectsList[i].ID, "error", err)
		}
	}
}

func (s *ProjectService) BackfillProjectImageRefs(ctx context.Context) (int, error) {
	var projectsList []Project
	if err := s.db.WithContext(ctx).
		Where("build_image_refs_json IS NULL").
		Find(&projectsList).Error; err != nil {
		return 0, fmt.Errorf("list projects for image ref backfill: %w", err)
	}
	for i := range projectsList {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		s.refreshProjectImageRefs(ctx, &projectsList[i])
	}
	return len(projectsList), nil
}

func (s *ProjectService) SyncProjectsFromFileSystem(ctx context.Context) error {
	// Serialized because the walk and the cleanup are two halves of one
	// decision: overlapping syncs let an older walk's cleanup delete a project a
	// newer walk had just upserted, because the older walk's `seen` set predates
	// it. Filesystem-watcher debounces fire these back to back.
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	followProjectSymlinks := s.settingsService.GetBoolSetting(ctx, "followProjectSymlinks", false)
	projectsDir, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		slog.WarnContext(ctx, "unable to prepare projects directory", "error", err)
		return nil
	}

	discoveredProjects, discoveryErr := projects.DiscoverProjectDirectories(ctx, projectsDir, followProjectSymlinks, s.config.ProjectScanMaxDepth)
	if discoveryErr != nil {
		if os.IsNotExist(discoveryErr) {
			return nil
		}
		return fmt.Errorf("Failed to discover projects in %q: %w", projectsDir, discoveryErr) //nolint:staticcheck // Preserve the existing error message.
	}

	renameSyncState := s.updates.SyncState(ctx)
	seen := map[string]struct{}{}
	for _, discoveredProject := range discoveredProjects {
		if _, renameTarget := renameSyncState.SkipDiscoveredPaths[filepath.Clean(discoveredProject.Path)]; renameTarget {
			continue
		}
		if uerr := s.upsertProjectForDir(ctx, discoveredProject.DirName, discoveredProject.Path); uerr != nil {
			slog.WarnContext(ctx, "failed to sync project from folder", "dir", discoveredProject.Path, "error", uerr)
			continue
		}
		seen[discoveredProject.Path] = struct{}{}
	}
	maps.Copy(seen, renameSyncState.ProtectSeenPaths)

	// Decide deletions before performing any, so the mass-wipe guard can veto a
	// suspicious pass (e.g. an unmounted projects volume) as a whole.
	var all []Project
	if findErr := s.db.WithContext(ctx).Select("id", "name", "dir_name", "path", "gitops_managed_by").Find(&all).Error; findErr != nil {
		slog.WarnContext(ctx, "error during DB cleanup of projects", "error", fmt.Errorf("list projects for cleanup failed: %w", findErr))
		return nil
	}
	candidates := 0
	var deletions, pendingDeletions []projectCleanupDecision
	for _, p := range all {
		if _, ok := seen[p.Path]; ok {
			continue
		}
		// GitOps owns these projects' lifecycle: their compose files may not exist
		// yet during a sync or after an SSH/clone failure.
		if gitOpsSyncID(&p) != "" {
			continue
		}
		// Rows imported from Arcane's own scratch dirs (project-update preview/backup,
		// GitOps stage/backup) or from filesystem snapshot/trash dirs are never real
		// projects. The decision is name-based, so it bypasses the mass-wipe guard.
		if projects.IsInternalScratchDirName(p.Name) || projects.IsInternalScratchDirName(filepath.Base(p.Path)) || projects.IsInternalScratchDirName(kit.FromPtr(p.DirName)) {
			deletions = append(deletions, projectCleanupDecision{project: p, reason: "removed internal Arcane scratch record (project-update/gitops temp dir)"})
			continue
		}
		if rel := listing.RelativePath(projectsDir, p.Path); rel != "" && projects.PathContainsSnapshotDirectory(rel) {
			deletions = append(deletions, projectCleanupDecision{project: p, reason: "removed project inside a filesystem snapshot/trash directory"})
			continue
		}
		candidates++
		// Projects beyond a lowered scan depth are no longer discovered; root-level and outside paths use the disk checks.
		if rel := listing.RelativePath(projectsDir, p.Path); s.config.ProjectScanMaxDepth > 0 && rel != "" && strings.Count(rel, "/") >= s.config.ProjectScanMaxDepth {
			pendingDeletions = append(pendingDeletions, projectCleanupDecision{project: p, reason: "removed project: directory is beyond the configured scan depth"})
			continue
		}
		validDir, statErr := projects.IsProjectDirectoryPath(p.Path, followProjectSymlinks)
		switch {
		case os.IsNotExist(statErr):
			pendingDeletions = append(pendingDeletions, projectCleanupDecision{project: p, reason: "removed project: directory no longer exists"})
			continue
		case statErr != nil:
			slog.WarnContext(ctx, "stat error during cleanup; keeping DB record", "path", p.Path, "error", statErr)
			continue
		case !validDir:
			pendingDeletions = append(pendingDeletions, projectCleanupDecision{project: p, reason: "removed project: path is no longer a valid project directory"})
			continue
		}
		// Only a directory with no compose file is pruned; an ambiguous, unreadable, or unparsable one is kept.
		_, resolveErr := s.ResolveProjectComposeFile(ctx, &p)
		switch {
		case errors.Is(resolveErr, common.ErrComposeFileNotFound):
			pendingDeletions = append(pendingDeletions, projectCleanupDecision{project: p, reason: "removed orphaned project: directory present but contains no compose file"})
		case resolveErr != nil:
			slog.WarnContext(ctx, "project directory present but compose file unresolved during cleanup; keeping DB record", "projectId", p.ID, "path", p.Path, "error", resolveErr)
		}
	}
	// Pruning more than one project and over half the candidates usually means an
	// unmounted or mis-mapped projects directory, so that pass keeps every record.
	if len(pendingDeletions) <= 1 || len(pendingDeletions)*2 <= candidates {
		deletions = append(deletions, pendingDeletions...)
	} else {
		slog.WarnContext(ctx,
			"skipping project cleanup: this reconcile would delete most projects in a single pass, which usually "+
				"means the projects directory is empty, unmounted, or mis-mapped; preserving DB records — check the "+
				"projects volume is mounted and mapped correctly",
			"wouldDelete", len(pendingDeletions),
			"cleanupCandidates", candidates,
			"projectsDir", projectsDir,
		)
	}

	// Every removal is logged so this destructive reconcile leaves an audit trail.
	for _, decision := range deletions {
		p := decision.project
		if deleteErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return deleteProjectWithTags(tx, p.ID) }); deleteErr != nil {
			slog.ErrorContext(ctx, "failed to delete project during filesystem cleanup", "projectId", p.ID, "name", p.Name, "path", p.Path, "reason", decision.reason, "error", deleteErr)
			continue
		}
		slog.WarnContext(ctx, "deleted project during filesystem cleanup", "projectId", p.ID, "name", p.Name, "path", p.Path, "reason", decision.reason)
	}
	return nil
}

func (s *ProjectService) upsertProjectForDir(ctx context.Context, dirName, dirPath string) error {
	var existing Project
	err := s.db.WithContext(ctx).Where("path = ?", dirPath).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("query existing project for %q failed: %w", dirPath, err)
	}

	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	composeMetadata, serviceCountErr := func() (projecttypes.ComposeIdentity, error) {
		projectsDirectory, dirErr := projects.GetProjectsDirectory(ctx, cfg.ProjectsDirectory.Value)
		if dirErr != nil {
			return projecttypes.ComposeIdentity{ResolvedProjectName: projects.NormalizeProjectName(dirName)}, dirErr
		}
		autoInjectEnv := kit.ParseOrDefault(cfg.AutoInjectEnv.Value, false, strconv.ParseBool)
		return projectsync.LoadComposeMetadata(ctx, s.composeIdentities, dirPath, dirName, projectsDirectory, autoInjectEnv, s.projectPathMapper(ctx))
	}()

	if errors.Is(err, gorm.ErrRecordNotFound) {
		existing = Project{
			Name:               composeMetadata.ResolvedProjectName,
			DirName:            new(dirName),
			Path:               dirPath,
			Status:             ProjectStatusUnknown,
			StatusReason:       new("Project discovered from filesystem, status pending Docker service query"),
			ServiceCount:       composeMetadata.ServiceCount,
			ComposeProjectName: composeMetadata.ComposeProjectName,
		}
		slog.InfoContext(ctx, "Discovered new project with unknown status", "project", dirName, "path", dirPath, "reason", *existing.StatusReason)
		if serviceCountErr != nil {
			slog.Log(ctx, kit.Ternary(errors.Is(serviceCountErr, common.ErrProjectEnvUnreadable), slog.LevelDebug, slog.LevelWarn),
				"failed to read compose service count during project discovery", "project", dirName, "path", dirPath, "error", serviceCountErr)
		}
		if cerr := s.db.WithContext(ctx).Create(&existing).Error; cerr != nil {
			return fmt.Errorf("create project for %q failed: %w", dirPath, cerr)
		}
	} else {
		updates := map[string]any{}
		if existing.Path != dirPath {
			updates["path"] = dirPath
		}
		if mo.PointerToOption(existing.DirName) != mo.Some(dirName) {
			updates["dir_name"] = dirName
		}
		if serviceCountErr != nil {
			slog.Log(ctx, kit.Ternary(errors.Is(serviceCountErr, common.ErrProjectEnvUnreadable), slog.LevelDebug, slog.LevelWarn),
				"failed to refresh compose service count during project sync", "projectId", existing.ID, "path", dirPath, "error", serviceCountErr)
		} else {
			if existing.ServiceCount != composeMetadata.ServiceCount {
				updates["service_count"] = composeMetadata.ServiceCount
			}
			if mo.PointerToOption(existing.ComposeProjectName) != mo.PointerToOption(composeMetadata.ComposeProjectName) {
				updates["compose_project_name"] = composeMetadata.ComposeProjectName
			}
			if composeMetadata.ExplicitProjectName && existing.Name != composeMetadata.ResolvedProjectName {
				updates["name"] = composeMetadata.ResolvedProjectName
			} else if !composeMetadata.ExplicitProjectName && projects.NormalizeProjectName(existing.Name) != existing.Name {
				updates["name"] = projects.NormalizeProjectName(existing.Name)
			}
		}
		if len(updates) == 0 {
			return s.reconcileComposeTagsForProject(ctx, &existing)
		}
		updates["updated_at"] = time.Now()
		if uerr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", existing.ID).Updates(updates).Error; uerr != nil {
			return fmt.Errorf("update project %s failed: %w", existing.ID, uerr)
		}
	}

	if serviceCountErr == nil && strings.TrimSpace(composeMetadata.ResolvedProjectName) != "" {
		var duplicates int64
		if countErr := s.db.WithContext(ctx).Model(&Project{}).
			Where("name = ? AND path <> ? AND id <> ?", composeMetadata.ResolvedProjectName, dirPath, existing.ID).
			Count(&duplicates).Error; countErr != nil {
			slog.WarnContext(ctx, "failed to check duplicate compose project names during project sync", "composeProjectName", composeMetadata.ResolvedProjectName, "path", dirPath, "error", countErr)
		} else if duplicates > 0 {
			slog.WarnContext(ctx, "multiple project directories resolve to the same compose project name", "composeProjectName", composeMetadata.ResolvedProjectName, "path", dirPath, "duplicates", duplicates)
		}
	}
	return s.reconcileComposeTagsForProject(ctx, &existing)
}

// ApplyGitSyncEnvToDirectory applies the same managed three-file environment
// merge used by single-file project syncs and returns the effective content
// before and after the update.
func (s *ProjectService) ApplyGitSyncEnvToDirectory(ctx context.Context, projectPath, projectsDirectory string, gitEnvContent *string) (before, after string, err error) {
	return projectsync.ApplyGitSyncEnv(ctx, projectPath, projectsDirectory, gitEnvContent)
}

func (s *ProjectService) reconcileComposeTagsForProject(ctx context.Context, projectModel *Project) error {
	if projectModel == nil {
		return nil
	}
	composeFile, err := s.ResolveProjectComposeFile(ctx, projectModel)
	if err != nil {
		return fmt.Errorf("resolve Compose file for tag reconciliation: %w", err)
	}
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return err
	}
	meta, err := projects.ParseArcaneComposeMetadata(ctx, composeFile, projectsDirectory, s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false))
	if err != nil {
		return err
	}
	if !meta.ProjectTagsAuthoritative {
		return nil
	}
	return s.reconcileComposeProjectTags(ctx, projectModel.ID, meta.ProjectTags)
}

func (s *ProjectService) UpdateProject(ctx context.Context, projectID string, name, composeContent, envContent, overrideContent *string, user usertypes.Actor) (*Project, error) {
	recoverErr := s.updates.RecoverProject(ctx, projectID)
	proj, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}
	if ensureErr := s.EnsureProjectPathUnderRoot(ctx, proj, false); ensureErr != nil {
		return nil, ensureErr
	}

	// A top-level `name:` in the compose file is authoritative; name-only renames check the on-disk compose so the API cannot bypass it.
	name = func() *string {
		switch {
		case composeContent != nil:
			yamlName := projects.ComposeContentProjectName(*composeContent)
			return kit.Ternary(yamlName != "", &yamlName, name)
		case name == nil:
			return nil
		}
		onDiskCompose, _, readErr := projects.ReadProjectFiles(ctx, proj.Path, "")
		if readErr != nil {
			return name
		}
		yamlName := projects.ComposeContentProjectName(onDiskCompose)
		return kit.Ternary(yamlName != "", &yamlName, name)
	}()
	newName := strings.TrimSpace(kit.FromPtr(name))
	if recoverErr != nil {
		if newName != "" && proj.Name != newName {
			return nil, recoverErr
		}
		slog.WarnContext(ctx, "project rename journal recovery failed before non-rename update; continuing", "projectId", projectID, "error", recoverErr)
	}
	if ensureProjectEnvReadableErr := update.EnsureProjectEnvReadable(ctx, projectsDirectory, proj.Path); ensureProjectEnvReadableErr != nil {
		return nil, ensureProjectEnvReadableErr
	}

	// A rename requires the stored and the live status to both be stopped.
	volumeMigration, err := func() (volume.Migration, error) {
		if newName == "" || proj.Name == newName {
			return nil, nil
		}
		if proj.Status != ProjectStatusStopped && proj.Status != ProjectStatusUnknown {
			return nil, fmt.Errorf("project must be stopped before renaming (current status: %s)", proj.Status)
		}
		services, servicesErr := s.projectServices(ctx, proj.ID)
		if servicesErr != nil {
			slog.WarnContext(ctx, "failed to resolve project status before rename", "projectId", proj.ID, "error", servicesErr)
			return nil, fmt.Errorf("project must be stopped before renaming (current status: %s): failed to verify live status: %w", proj.Status, servicesErr)
		}
		if status := ProjectStatus(listing.ProjectStatus(services)); status != ProjectStatusStopped {
			return nil, fmt.Errorf("project must be stopped before renaming (current status: %s)", status)
		}
		serviceCount, runningCount := listing.ServiceCounts(services)
		*proj = Project{ //exhaustruct:enforce
			BaseModel:          proj.BaseModel,
			Name:               proj.Name,
			DirName:            proj.DirName,
			Path:               proj.Path,
			Status:             ProjectStatusStopped,
			StatusReason:       nil,
			ServiceCount:       serviceCount,
			RunningCount:       runningCount,
			GitOpsManagedBy:    proj.GitOpsManagedBy,
			ComposeProjectName: proj.ComposeProjectName,
			ImageRefsJSON:      proj.ImageRefsJSON,
			BuildImageRefsJSON: proj.BuildImageRefsJSON,
			IsArchived:         proj.IsArchived,
			ArchivedAt:         proj.ArchivedAt,
		}
		return s.prepareProjectRenameVolumeMigration(ctx, proj, newName, projectsDirectory, composeContent, envContent, overrideContent)
	}()
	if err != nil {
		return nil, err
	}

	renameJournal := s.updates.Prepare(proj.ID, proj.Name, proj.Path, proj.DirName, name, projectsDirectory, volumeMigration)
	backup, cleanupBackup, err := func() (*projects.ProjectUpdateBackup, func(), error) {
		if composeContent == nil && envContent == nil && overrideContent == nil {
			return nil, func() {}, nil
		}
		return projects.BackupProjectDirectory(ctx, projectsDirectory, proj.Path, ".project-update-backup-*", projects.ProjectUpdateBackupScope{TopLevelFiles: true})
	}()
	if err != nil {
		return nil, err
	}
	defer cleanupBackup()

	if renameJournal != nil {
		if writeProjectRenameJournalErr := s.updates.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseStarted); writeProjectRenameJournalErr != nil {
			return nil, writeProjectRenameJournalErr
		}
	}

	original := *proj
	if updateErr := s.applyProjectUpdate(ctx, proj, newName, projectsDirectory, composeContent, envContent, overrideContent, volumeMigration, renameJournal); updateErr != nil {
		return nil, s.updates.RollbackFailedUpdate(ctx, projectID, projectsDirectory, original.Path, proj.Path, backup, renameJournal != nil, updateErr)
	}

	if composeContent != nil || overrideContent != nil {
		s.refreshComposeProjectName(ctx, proj)
		s.refreshProjectImageRefs(ctx, proj)
		if reconcileErr := s.reconcileComposeTagsForProject(ctx, proj); reconcileErr != nil {
			slog.WarnContext(ctx, "failed to reconcile Compose project tags after project content update", "projectId", proj.ID, "error", reconcileErr)
		}
		if countsErr := s.updateProjectStatusAndCounts(ctx, proj.ID, proj.Status); countsErr != nil {
			slog.WarnContext(ctx, "failed to update service counts after project content update", "projectId", proj.ID, "error", countsErr)
		}
	}
	metadata := database.JSON{"action": "update", "projectID": proj.ID, "projectName": proj.Name}
	if composeContent != nil {
		metadata["composeUpdated"] = true
	}
	if envContent != nil {
		metadata["envUpdated"] = true
	}
	if overrideContent != nil {
		metadata["overrideUpdated"] = true
	}
	s.logProjectEvent(ctx, event.EventTypeProjectUpdate, proj.ID, proj.Name, user, metadata, "could not log project update action")
	if composeContent != nil || envContent != nil || overrideContent != nil {
		s.FilesChanged.Publish(proj.ID)
	}

	slog.InfoContext(ctx, "project updated", "projectId", proj.ID, "name", proj.Name)
	return proj, nil
}

// applyProjectUpdate renames, persists files, migrates volumes, and
// saves the row; a failure before the save rolls an applied volume migration back.
func (s *ProjectService) applyProjectUpdate(
	ctx context.Context,
	proj *Project,
	newName, projectsDirectory string,
	composeContent, envContent, overrideContent *string,
	volumeMigration volume.Migration,
	renameJournal *projecttypes.RenameJournal,
) (err error) {
	volumeMigrationApplied := false
	defer func() {
		if err != nil && volumeMigrationApplied {
			if rollbackErr := volumeMigration.Rollback(ctx); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("failed to rollback project volume rename: %w", rollbackErr))
			}
		}
	}()

	// A rename moves a stopped project's directory to its sanitized new name under the projects directory.
	if newName != "" && proj.Name != newName {
		newDirName := projects.SanitizeProjectName(newName)
		targetPath := filepath.Clean(filepath.Join(projectsDirectory, newDirName))
		if filepath.Clean(proj.Path) != targetPath {
			targetLogical, logicalErr := acfs.LogicalPath(projectsDirectory, targetPath)
			if logicalErr != nil {
				return fmt.Errorf("failed to resolve project directory rename target: %w", logicalErr)
			}
			// An imported project outside the projects directory crosses roots, so it cannot be a confined rename;
			// that move checks cancellation itself, as acfs.Rename would.
			if renameErr := func() error {
				currentLogical, currentErr := acfs.LogicalPath(projectsDirectory, filepath.Clean(proj.Path))
				if currentErr == nil {
					return acfs.Rename(ctx, projectsDirectory, currentLogical, targetLogical)
				}
				if cancellationErr := ctx.Err(); cancellationErr != nil {
					return cancellationErr
				}
				return os.Rename(filepath.Clean(proj.Path), targetPath)
			}(); renameErr != nil {
				return fmt.Errorf("failed to rename project directory: %w", renameErr)
			}
		}
		*proj = Project{ //exhaustruct:enforce
			BaseModel:          proj.BaseModel,
			Name:               newName,
			DirName:            &newDirName,
			Path:               kit.Ternary(filepath.Clean(proj.Path) != targetPath, targetPath, proj.Path),
			Status:             proj.Status,
			StatusReason:       proj.StatusReason,
			ServiceCount:       proj.ServiceCount,
			RunningCount:       proj.RunningCount,
			GitOpsManagedBy:    proj.GitOpsManagedBy,
			ComposeProjectName: proj.ComposeProjectName,
			ImageRefsJSON:      proj.ImageRefsJSON,
			BuildImageRefsJSON: proj.BuildImageRefsJSON,
			IsArchived:         proj.IsArchived,
			ArchivedAt:         proj.ArchivedAt,
		}
	}
	if persistFilesErr := projectsync.PersistUpdatedProjectFiles(ctx, proj.Path, proj.Name, projectsDirectory, composeContent, envContent, overrideContent); persistFilesErr != nil {
		return persistFilesErr
	}
	if migrationErr := projects.ApplyRenameVolumeMigration(ctx, s.updates.Operations(), volumeMigration, renameJournal, &volumeMigrationApplied); migrationErr != nil {
		return migrationErr
	}
	if saveErr := s.db.WithContext(ctx).Save(proj).Error; saveErr != nil {
		return fmt.Errorf("failed to update project: %w", saveErr)
	}
	projects.FinalizeRenameAfterCommit(ctx, s.updates.Operations(), proj.ID, volumeMigration, renameJournal, new(renameJournal != nil))
	return nil
}

func (s *ProjectService) ApplyGitSyncProjectFiles(
	ctx context.Context,
	projectID, composeContent string,
	gitEnvContent, gitOverrideContent *string,
	gitOverrideFileName string,
	user usertypes.Actor,
) (*Project, bool, error) {
	proj, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return nil, false, err
	}
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get projects directory: %w", err)
	}
	if ensureErr := s.EnsureProjectPathUnderRoot(ctx, proj, false); ensureErr != nil {
		return nil, false, ensureErr
	}
	beforeCompose, beforeEnv, beforeOverride, beforeErr := s.projectContent(ctx, proj.ID)

	envUpdate, err := projectsync.PrepareGitSyncEnvUpdate(proj.Path, gitEnvContent)
	if err != nil {
		return nil, false, fmt.Errorf("failed to resolve git env state: %w", err)
	}

	if validateComposeContentForUpdateErr := projects.ValidateComposeContentForUpdate(
		ctx,
		projectsDirectory,
		proj.Path,
		proj.Name,
		composeContent,
		envUpdate.EffectiveContent,
		gitOverrideContent,
		gitOverrideFileName,
		true,
	); validateComposeContentForUpdateErr != nil {
		return nil, false, fmt.Errorf("invalid compose file: %w", validateComposeContentForUpdateErr)
	}

	backup, cleanupBackup, err := projects.BackupProjectDirectory(ctx, projectsDirectory, proj.Path, ".project-update-backup-*", projects.ProjectUpdateBackupScope{TopLevelFiles: true})
	if err != nil {
		return nil, false, err
	}
	defer cleanupBackup()

	// The env is persisted first so WriteComposeFile targets the COMPOSE_FILE base the updated .env selects.
	// A failure before the row save restores the backup, so new env values never pair with an old compose file set.
	if applyErr := func() error {
		if envErr := projectsync.PersistGitSyncEnvFiles(ctx, proj.Path, projectsDirectory, envUpdate); envErr != nil {
			return fmt.Errorf("failed to sync git env files: %w", envErr)
		}
		if composeErr := projects.WriteComposeFile(ctx, projectsDirectory, proj.Path, composeContent); composeErr != nil {
			return fmt.Errorf("failed to save compose file: %w", composeErr)
		}
		if overrideErr := projects.WriteComposeOverrideFile(ctx, projectsDirectory, proj.Path, gitOverrideContent, gitOverrideFileName); overrideErr != nil {
			return fmt.Errorf("failed to sync git override file: %w", overrideErr)
		}
		if saveErr := s.db.WithContext(ctx).Save(proj).Error; saveErr != nil {
			return fmt.Errorf("failed to update project: %w", saveErr)
		}
		return nil
	}(); applyErr != nil {
		if restoreErr := projects.RestoreProjectDirectoryBackup(ctx, projectsDirectory, proj.Path, backup); restoreErr != nil {
			return nil, false, errors.Join(applyErr, fmt.Errorf("failed to restore project files after update failure: %w", restoreErr))
		}
		return nil, false, applyErr
	}

	s.refreshComposeProjectName(ctx, proj)
	s.refreshProjectImageRefs(ctx, proj)
	if reconcileErr := s.reconcileComposeTagsForProject(ctx, proj); reconcileErr != nil {
		slog.WarnContext(ctx, "failed to reconcile Compose project tags after project content update", "projectId", proj.ID, "error", reconcileErr)
	}
	if countsErr := s.updateProjectStatusAndCounts(ctx, proj.ID, proj.Status); countsErr != nil {
		slog.WarnContext(ctx, "failed to update service counts after project content update", "projectId", proj.ID, "error", countsErr)
	}

	// Unreadable content on either side always compares as changed.
	afterCompose, afterEnv, afterOverride, afterErr := s.projectContent(ctx, proj.ID)
	if beforeErr != nil || afterErr != nil {
		slog.WarnContext(ctx, "failed to read project content for git sync change detection; treating as changed", "projectId", proj.ID, "error", errors.Join(beforeErr, afterErr))
	}
	contentChanged := beforeErr != nil || afterErr != nil || beforeCompose != afterCompose || projects.EnvContentChanged(beforeEnv, afterEnv) || beforeOverride != afterOverride
	if contentChanged || (gitEnvContent == nil && envUpdate.State.HasGitSource) {
		metadata := database.JSON{
			"action":          "git_sync_update",
			"projectID":       proj.ID,
			"projectName":     proj.Name,
			"composeUpdated":  beforeErr != nil || afterErr != nil || beforeCompose != afterCompose,
			"envUpdated":      beforeErr != nil || afterErr != nil || projects.EnvContentChanged(beforeEnv, afterEnv),
			"overrideUpdated": beforeErr != nil || afterErr != nil || beforeOverride != afterOverride,
		}
		if gitEnvContent == nil && envUpdate.State.HasGitSource {
			metadata["envSourceRemoved"] = true
		}
		s.logProjectEvent(ctx, event.EventTypeProjectUpdate, proj.ID, proj.Name, user, metadata, "could not log git sync project update action")
	}

	return proj, contentChanged, nil
}

// prepareProjectRenameVolumeMigration rejects an unusable rename target, then plans the volume rename for a stopped
// project. Content changes are applied to a scratch preview first so the plan reflects the updated compose files.
func (s *ProjectService) prepareProjectRenameVolumeMigration(
	ctx context.Context,
	proj *Project,
	newName, projectsDirectory string,
	composeContent, envContent, overrideContent *string,
) (volume.Migration, error) {
	newDirName := projects.SanitizeProjectName(newName)
	if newDirName == "" || strings.Trim(newDirName, "_") == "" {
		return nil, errors.New("invalid project name: results in empty directory name")
	}
	if targetPath := filepath.Clean(filepath.Join(projectsDirectory, newDirName)); filepath.Clean(proj.Path) != targetPath {
		targetLogical, logicalErr := acfs.LogicalPath(projectsDirectory, targetPath)
		if logicalErr != nil {
			return nil, fmt.Errorf("failed to resolve project directory rename target: %w", logicalErr)
		}
		exists, existsErr := acfs.Exists(ctx, projectsDirectory, targetLogical)
		if existsErr != nil {
			return nil, fmt.Errorf("failed to check project directory rename target: %w", existsErr)
		}
		if exists {
			return nil, fmt.Errorf("project directory already exists: %s", targetPath)
		}
	}
	target := proj
	if composeContent != nil || envContent != nil || overrideContent != nil {
		previewLogical, err := acfs.MkdirTemp(ctx, projectsDirectory, "/", ".project-update-preview-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create project update preview: %w", err)
		}
		previewPath := filepath.Join(projectsDirectory, filepath.FromSlash(strings.TrimPrefix(previewLogical, "/")))
		defer func() {
			// acfs refuses an already-cancelled context, so cleanup runs detached
			// or a cancelled update leaks the preview directory.
			cleanupCtx := context.WithoutCancel(ctx)
			if removeErr := acfs.RemoveAll(cleanupCtx, projectsDirectory, previewLogical); removeErr != nil {
				slog.WarnContext(cleanupCtx, "failed to remove project update preview", "path", previewPath, "error", removeErr)
			}
		}()

		if _, copyDirErr := acfs.CopyDir(ctx, proj.Path, previewPath, acfstypes.CopyOptions{}); copyDirErr != nil {
			return nil, fmt.Errorf("failed to prepare project update preview: %w", copyDirErr)
		}
		previewProject := Project{ //exhaustruct:enforce
			BaseModel:          proj.BaseModel,
			Name:               proj.Name,
			DirName:            proj.DirName,
			Path:               previewPath,
			Status:             proj.Status,
			StatusReason:       proj.StatusReason,
			ServiceCount:       proj.ServiceCount,
			RunningCount:       proj.RunningCount,
			GitOpsManagedBy:    proj.GitOpsManagedBy,
			ComposeProjectName: proj.ComposeProjectName,
			ImageRefsJSON:      proj.ImageRefsJSON,
			BuildImageRefsJSON: proj.BuildImageRefsJSON,
			IsArchived:         proj.IsArchived,
			ArchivedAt:         proj.ArchivedAt,
		}
		if persistErr := projectsync.PersistUpdatedProjectFiles(ctx, previewProject.Path, previewProject.Name, projectsDirectory, composeContent, envContent, overrideContent); persistErr != nil {
			return nil, fmt.Errorf("failed to prepare project update preview: %w", persistErr)
		}
		target = &previewProject
	}

	oldComposeName, newComposeName := projects.NormalizeProjectName(target.Name), projects.NormalizeProjectName(newName)
	if target.Status != ProjectStatusStopped || oldComposeName == "" || newComposeName == "" || oldComposeName == newComposeName {
		return nil, nil
	}

	composeProject, _, err := s.loadComposeProjectForProject(ctx, target, nil)
	if err != nil {
		if errors.Is(err, common.ErrProjectComposeFileNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load compose project for volume rename: %w", err)
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker for volume rename: %w", err)
	}

	return projects.PlanVolumeMigration(ctx, dockerClient, composeProject, oldComposeName, newComposeName, volumehelper.ToolsImage(s.settingsService.GetSettingsConfig().ToolsImageRegistry.Value))
}

// UpdateProjectServiceImages persists selected service image tags before recreating them.
func (s *ProjectService) UpdateProjectServiceImages(ctx context.Context, projectID string, changes map[string]updatertypes.ServiceImageChange, user usertypes.Actor) error {
	services, err := s.SaveProjectServiceImages(ctx, projectID, changes)
	if err != nil {
		return err
	}
	// Keep the desired source on deployment failure: Compose may have partially
	// recreated services, and the pending update must remain retryable.
	return s.UpdateProjectServices(ctx, projectID, services, user, false)
}

// SaveProjectServiceImages persists selected service image tags without
// deploying them and returns the changed services.
func (s *ProjectService) SaveProjectServiceImages(ctx context.Context, projectID string, changes map[string]updatertypes.ServiceImageChange) ([]string, error) {
	if len(changes) == 0 {
		return nil, errors.New("service image changes are required")
	}
	proj, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if gitOpsSyncID(proj) != "" {
		return nil, errors.New("tag updates cannot edit a GitOps-managed project; update image tags in the source repository")
	}
	// Selecting the changed services keeps profile-gated ones active.
	effective, _, err := s.loadComposeProjectForProject(ctx, proj, nil, slices.Collect(maps.Keys(changes))...)
	if err != nil {
		return nil, fmt.Errorf("load project for tag update: %w", err)
	}
	services, err := s.updates.ApplyImageChanges(ctx, proj.Path, effective, changes)
	if err != nil {
		return nil, err
	}
	s.invalidateProjectCaches(projectID)
	return services, nil
}

func (s *ProjectService) GetProjectWorkspace(ctx context.Context, projectID string) (*workspacetypes.Workspace, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if ensureProjectPathUnderRootErr := s.EnsureProjectPathUnderRoot(ctx, proj, false); ensureProjectPathUnderRootErr != nil {
		return nil, ensureProjectPathUnderRootErr
	}
	ownedPaths, ownedErr := s.gitOpsOwnedWorkspacePaths(ctx, proj)
	return s.workspace.Read(ctx, proj.Path, s.workspaceComposeFileName(ctx, proj), kit.Ternary(ownedErr == nil, ownedPaths, nil))
}

func (s *ProjectService) GetProjectWorkspaceFile(ctx context.Context, projectID, relativePath string) (*workspacetypes.FileContent, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ownedPaths, ownedErr := s.gitOpsOwnedWorkspacePaths(ctx, proj)
	return s.workspace.File(ctx, proj.Path, s.workspaceComposeFileName(ctx, proj), relativePath, kit.Ternary(ownedErr == nil, ownedPaths, nil))
}

func (s *ProjectService) DownloadProjectWorkspaceFile(ctx context.Context, projectID, relativePath string) (io.ReadCloser, int64, string, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, 0, "", err
	}
	return s.workspace.Download(ctx, proj.Path, s.workspaceComposeFileName(ctx, proj), relativePath)
}

func (s *ProjectService) UpdateProjectWorkspace(
	ctx context.Context,
	projectID string,
	manifest projecttypes.WorkspaceUpdateManifest,
	uploads map[int][]byte,
	user usertypes.Actor,
) (*workspacetypes.Workspace, error) {
	if err := workspacepkg.ValidateUpdateManifest(manifest.FileTreeRevision, len(manifest.FileChanges), 500); err != nil {
		return nil, common.Classify(common.ErrProjectWorkspaceBadRequest, err)
	}
	proj, err := s.getMutableProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// Only the paths the GitOps sync owns are locked; the rest of the
	// directory is operator-owned overlay (e.g. secret env files a public
	// repo cannot carry) and stays editable (#3634).
	ownedPaths, err := s.gitOpsOwnedWorkspacePaths(ctx, proj)
	if err != nil {
		return nil, err
	}
	if validateWorkspaceChangesAgainstGitOpsErr := workspace.ValidateWorkspaceChangesAgainstGitOps(manifest.FileChanges, ownedPaths); validateWorkspaceChangesAgainstGitOpsErr != nil {
		return nil, validateWorkspaceChangesAgainstGitOpsErr
	}
	if ensureProjectPathUnderRootErr := s.EnsureProjectPathUnderRoot(ctx, proj, true); ensureProjectPathUnderRootErr != nil {
		return nil, ensureProjectPathUnderRootErr
	}

	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, err
	}
	if ensureProjectEnvReadableErr := update.EnsureProjectEnvReadable(ctx, projectsDirectory, proj.Path); ensureProjectEnvReadableErr != nil {
		return nil, ensureProjectEnvReadableErr
	}
	if applyErr := s.workspace.Apply(ctx, projectsDirectory, proj.Path, s.workspaceComposeFileName(ctx, proj), manifest, uploads); applyErr != nil {
		return nil, applyErr
	}

	s.refreshProjectImageRefs(ctx, proj)
	if updateProjectStatusandCountsErr := s.updateProjectStatusAndCounts(ctx, proj.ID, proj.Status); updateProjectStatusandCountsErr != nil {
		return nil, fmt.Errorf("refresh project after workspace update: %w", updateProjectStatusandCountsErr)
	}
	s.logProjectEvent(ctx, event.EventTypeProjectUpdate, proj.ID, proj.Name, user, database.JSON{
		"action":          "update_project_workspace",
		"fileChangeCount": len(manifest.FileChanges),
	}, "could not log project workspace update")
	s.FilesChanged.Publish(proj.ID)
	return s.GetProjectWorkspace(ctx, projectID)
}

func (s *ProjectService) workspaceComposeFileName(ctx context.Context, proj *Project) string {
	if composeFile, err := s.ResolveProjectComposeFile(ctx, proj); err == nil {
		return filepath.Base(composeFile)
	}
	return projects.DefaultComposeFileName
}

// gitOpsOwnedWorkspacePaths returns the workspace-relative paths owned
// by the project's GitOps sync. Returns nil for projects without a live sync,
// including a stale gitops_managed_by marker.
func (s *ProjectService) gitOpsOwnedWorkspacePaths(ctx context.Context, proj *Project) (map[string]struct{}, error) {
	if gitOpsSyncID(proj) == "" {
		return nil, nil
	}
	var syncRecord GitOpsSync
	switch err := s.db.WithContext(ctx).Where("project_id = ?", proj.ID).First(&syncRecord).Error; {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("load gitops sync for workspace: %w", err)
	}
	return workspace.OwnedPaths(syncRecord.SyncedFiles, syncRecord.ComposePath)
}

func (s *ProjectService) projectServices(ctx context.Context, projectID string) ([]projecttypes.RuntimeService, error) {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	composeProject, composeFileFullPath, derr := s.loadComposeProjectForProject(ctx, projectFromDb, nil)
	if errors.Is(derr, common.ErrProjectEnvUnreadable) {
		// The Compose file cannot be loaded, so derive services from labeled containers.
		containers, listErr := s.details.ComposeContainers(ctx)
		if listErr != nil {
			return nil, listErr
		}
		meta := s.ProjectMetadata(ctx, *projectFromDb, nil)
		matched := listing.ProjectContainers(projectRecord(*projectFromDb), listing.GroupComposeContainersByProject(containers))
		currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
		services := make([]projecttypes.RuntimeService, 0, len(matched))
		for _, c := range matched {
			services = append(services, listing.RuntimeServiceFromContainer(IconCatalogForContext(ctx), c, meta, currentContainerID, currentContainerErr))
		}
		return services, nil
	}
	if derr != nil {
		return []projecttypes.RuntimeService{}, fmt.Errorf("failed to load compose project in %s: %w", projectFromDb.Path, derr)
	}

	projectsDirectory, projectsDirErr := s.GetProjectsDirectory(ctx)
	if projectsDirErr != nil {
		slog.WarnContext(ctx, "failed to resolve projects directory for Arcane compose metadata", "path", composeFileFullPath, "error", projectsDirErr)
	}
	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	return s.details.ComposeServices(ctx, composeProject, composeFileFullPath, projectsDirectory, autoInjectEnv, IconCatalogForContext(ctx))
}

func (s *ProjectService) StreamProjectLogs(ctx context.Context, projectID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	return projectdetails.Logs(ctx, proj.Name, logsChan, follow, tail, since, timestamps)
}

func (s *ProjectService) GetProjectStatusCounts(ctx context.Context) (projecttypes.StatusCounts, error) {
	// Runtime status comes from containers, so only the matching and fallback columns are read.
	var projectsList []Project
	if listProjectsErr := s.db.WithContext(ctx).Select("id", "name", "path", "status", "compose_project_name", "is_archived").Find(&projectsList).Error; listProjectsErr != nil {
		return projecttypes.StatusCounts{}, fmt.Errorf("failed to list projects: %w", listProjectsErr)
	}
	return s.listing.StatusCounts(ctx, projectRecords(projectsList)), nil
}

func (s *ProjectService) AggregateProjects(ctx context.Context, params pagination.QueryParams) (base.Paginated[projecttypes.Details], error) {
	collection, err := httpx.CollectEnvironments[base.Paginated[projecttypes.Details]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return base.Paginated[projecttypes.Details]{}, fmt.Errorf("collect environment projects: %w", err)
	}
	pages := make([]pagination.EnvironmentPage[projecttypes.Details], 0, len(collection.Results))
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[projecttypes.Details]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
	}
	byID := func(a, b projecttypes.Details) int { return strings.Compare(a.ID, b.ID) }
	listConfig := listing.PaginationConfig()
	items, resp := pagination.MergeEnvironmentPages(pagination.Config[projecttypes.Details]{
		SearchAccessors: listConfig.SearchAccessors,
		SortBindings:    append([]pagination.SortBinding[projecttypes.Details]{{Key: "id", Fn: byID, DescFn: byID}}, listConfig.SortBindings...),
		FilterAccessors: listConfig.FilterAccessors,
	}, pages, params)
	return base.Paginated[projecttypes.Details]{
		Success:            true,
		Data:               items,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
}

func (s *ProjectService) AggregateProjectCounts(ctx context.Context) (base.ApiResponse[projecttypes.StatusCounts], error) {
	collection, err := httpx.CollectEnvironments[base.ApiResponse[projecttypes.StatusCounts]](ctx, nil)
	if err != nil {
		return base.ApiResponse[projecttypes.StatusCounts]{}, fmt.Errorf("collect environment project counts: %w", err)
	}
	var running, stopped, total, archived int
	for _, result := range collection.Results {
		running += result.Value.Data.RunningProjects
		stopped += result.Value.Data.StoppedProjects
		total += result.Value.Data.TotalProjects
		archived += result.Value.Data.ArchivedProjects
	}
	return base.ApiResponse[projecttypes.StatusCounts]{
		Success: true,
		Data: projecttypes.StatusCounts{
			RunningProjects:  running,
			StoppedProjects:  stopped,
			TotalProjects:    total,
			ArchivedProjects: archived,
		},
		FailedEnvironments: collection.Failures,
	}, nil
}

func (s *ProjectService) AggregateProjectTags(ctx context.Context) (base.ApiResponse[[]projecttypes.TagOption], error) {
	collection, err := httpx.CollectEnvironments[base.ApiResponse[[]projecttypes.TagOption]](ctx, nil)
	if err != nil {
		return base.ApiResponse[[]projecttypes.TagOption]{}, fmt.Errorf("collect environment project tags: %w", err)
	}
	options := make([]projecttypes.TagOption, 0)
	for _, result := range collection.Results {
		for _, option := range result.Value.Data {
			if !slices.ContainsFunc(options, func(existing projecttypes.TagOption) bool { return existing.Name == option.Name }) {
				options = append(options, option)
			}
		}
	}
	slices.SortStableFunc(options, func(a, b projecttypes.TagOption) int { return strings.Compare(a.Name, b.Name) })
	return base.ApiResponse[[]projecttypes.TagOption]{Success: true, Data: options, FailedEnvironments: collection.Failures}, nil
}

// GetProjectTags returns the effective UI and Compose tag associations for a project.
func (s *ProjectService) GetProjectTags(ctx context.Context, projectID string) ([]projecttypes.Tag, error) {
	var rows []projecttypes.TagAssignment
	if err := s.db.WithContext(ctx).Model(&ProjectTag{}).Where("project_id = ?", projectID).Order("name, source, color").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load project tags: %w", err)
	}
	return tags.Group(rows)[projectID], nil
}

// ListProjectTagOptions returns the distinct tag names and colors available in the current environment.
func (s *ProjectService) ListProjectTagOptions(ctx context.Context) ([]projecttypes.TagOption, error) {
	var rows []projecttypes.TagAssignment
	if err := s.db.WithContext(ctx).Model(&ProjectTag{}).Order("name, source DESC, color").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list project tag options: %w", err)
	}
	// Rows are ordered by name with the UI source first, so each name's first row wins.
	options := make([]projecttypes.TagOption, 0)
	for _, row := range rows {
		if len(options) == 0 || options[len(options)-1].Name != row.Name {
			options = append(options, projecttypes.TagOption{Name: row.Name, Color: row.Color})
		}
	}
	return options, nil
}

// ListProjectReferences returns the ID and name of every project, including archived ones, without runtime state.
func (s *ProjectService) ListProjectReferences(ctx context.Context) ([]projecttypes.Reference, error) {
	references := []projecttypes.Reference{}
	if err := s.db.WithContext(ctx).Model(&Project{}).Select("id", "name").Order("name").Find(&references).Error; err != nil {
		return nil, fmt.Errorf("list project references: %w", err)
	}
	return references, nil
}

// UpdateProjectTag attaches or detaches a UI-managed tag and rejects Compose-owned names.
func (s *ProjectService) UpdateProjectTag(ctx context.Context, projectID, name string, color projecttypes.TagColor, attached bool, user usertypes.Actor) ([]projecttypes.Tag, error) {
	normalized, normalizedColor, err := tags.NormalizeUpdate(name, color, attached)
	if err != nil {
		return nil, err
	}

	var projectModel Project
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if findProjectErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&projectModel, "id = ?", projectID).Error; findProjectErr != nil {
			return fmt.Errorf("find project for tag update: %w", findProjectErr)
		}
		return tags.ApplyUpdate(tagStore{tx: tx}, projectID, normalized, normalizedColor, attached)
	})
	if err != nil {
		return nil, err
	}

	metadata := database.JSON{"action": "update_tags", "projectID": projectID, "projectName": projectModel.Name, "tag": normalized, "attached": attached}
	s.logProjectEvent(ctx, event.EventTypeProjectUpdate, projectID, projectModel.Name, user, metadata, "could not log project tag update")
	return s.GetProjectTags(ctx, projectID)
}

func (s *ProjectService) reconcileComposeProjectTags(ctx context.Context, projectID string, composeTags []projecttypes.TagOption) error {
	normalized, err := tags.NormalizeComposeProjectTags(composeTags)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var projectModel Project
		if findProjectErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&projectModel, "id = ?", projectID).Error; findProjectErr != nil {
			return fmt.Errorf("find project for Compose tag reconciliation: %w", findProjectErr)
		}
		return tags.ReplaceCompose(tagStore{tx: tx}, projectID, normalized)
	})
}
