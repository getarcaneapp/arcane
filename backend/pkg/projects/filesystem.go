package projects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

type ProjectFilesystemConfig struct {
	ProjectsRoot  string
	AutoInjectEnv bool
	PathMapper    *PathMapper
}

type ProjectWorkspace struct {
	Name            string
	DirName         string
	Path            string
	ComposeFileName string
}

type StagedProjectWorkspace struct {
	Current ProjectWorkspace
	Stage   ProjectWorkspace
	Target  ProjectWorkspace
}

type ProjectFilesystem struct {
	Config     ProjectFilesystemConfig
	renamePath func(oldPath, newPath string) error
}

func NewProjectFilesystem(config ProjectFilesystemConfig) *ProjectFilesystem {
	config.ProjectsRoot = filepath.Clean(config.ProjectsRoot)
	return &ProjectFilesystem{Config: config, renamePath: os.Rename}
}

func (fs *ProjectFilesystem) ResolveExisting(workspace ProjectWorkspace) (ProjectWorkspace, error) {
	if fs == nil {
		return ProjectWorkspace{}, fmt.Errorf("project filesystem is nil")
	}
	if strings.TrimSpace(fs.Config.ProjectsRoot) == "" {
		return ProjectWorkspace{}, fmt.Errorf("projects root is required")
	}

	workspace.Name = strings.TrimSpace(workspace.Name)
	workspace.DirName = strings.TrimSpace(workspace.DirName)
	workspace.ComposeFileName = strings.TrimSpace(workspace.ComposeFileName)

	if workspace.DirName == "" {
		switch {
		case workspace.Path != "":
			workspace.DirName = filepath.Base(filepath.Clean(workspace.Path))
		case workspace.Name != "":
			workspace.DirName = SanitizeProjectName(workspace.Name)
		}
	}

	if workspace.DirName == "" || strings.Trim(workspace.DirName, "_") == "" {
		return ProjectWorkspace{}, fmt.Errorf("invalid project workspace: missing directory name")
	}

	path := strings.TrimSpace(workspace.Path)
	if path == "" {
		path = filepath.Join(fs.Config.ProjectsRoot, workspace.DirName)
	}

	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return ProjectWorkspace{}, fmt.Errorf("resolve project path: %w", err)
	}
	pathAbs = filepath.Clean(pathAbs)

	rootAbs, err := filepath.Abs(fs.Config.ProjectsRoot)
	if err != nil {
		return ProjectWorkspace{}, fmt.Errorf("resolve projects root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	if !IsSafeSubdirectory(rootAbs, pathAbs) {
		pathAbs = filepath.Join(rootAbs, workspace.DirName)
	}

	workspace.Path = pathAbs
	return workspace, nil
}

func (fs *ProjectFilesystem) CreateWorkspace(name string) (ProjectWorkspace, error) {
	if fs == nil {
		return ProjectWorkspace{}, fmt.Errorf("project filesystem is nil")
	}

	sanitized := SanitizeProjectName(name)
	basePath := filepath.Join(fs.Config.ProjectsRoot, sanitized)
	path, dirName, err := CreateUniqueDir(fs.Config.ProjectsRoot, basePath, name, 0o755)
	if err != nil {
		return ProjectWorkspace{}, err
	}

	return ProjectWorkspace{
		Name:    name,
		DirName: dirName,
		Path:    path,
	}, nil
}

func (fs *ProjectFilesystem) ResolveComposeFile(workspace ProjectWorkspace) (string, error) {
	workspace, err := fs.ResolveExisting(workspace)
	if err != nil {
		return "", err
	}

	if workspace.ComposeFileName != "" {
		candidate := filepath.Join(workspace.Path, filepath.Base(workspace.ComposeFileName))
		if info, statErr := os.Stat(candidate); statErr == nil {
			if !info.IsDir() {
				return candidate, nil
			}
		} else if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect compose file %s: %w", candidate, statErr)
		}
	}

	return DetectComposeFile(workspace.Path)
}

func (fs *ProjectFilesystem) LoadComposeProject(ctx context.Context, workspace ProjectWorkspace) (*composetypes.Project, string, error) {
	workspace, err := fs.ResolveExisting(workspace)
	if err != nil {
		return nil, "", err
	}

	composeFile, err := fs.ResolveComposeFile(workspace)
	if err != nil {
		return nil, "", err
	}

	projectName := loader.NormalizeProjectName(strings.TrimSpace(workspace.Name))
	project, err := LoadComposeProject(ctx, composeFile, projectName, fs.Config.ProjectsRoot, fs.Config.AutoInjectEnv, fs.Config.PathMapper)
	if err != nil {
		return nil, "", err
	}

	return project, composeFile, nil
}

func (fs *ProjectFilesystem) LoadComposeMetadata(ctx context.Context, workspace ProjectWorkspace) (serviceCount int, composeProjectName *string, err error) {
	workspace, err = fs.ResolveExisting(workspace)
	if err != nil {
		return 0, nil, err
	}

	composeFile, err := fs.ResolveComposeFile(workspace)
	if err != nil {
		return 0, nil, err
	}

	normalizedName := loader.NormalizeProjectName(strings.TrimSpace(workspace.Name))
	project, loadErr := loadComposeProjectInternal(ctx, composeFile, "", fs.Config.ProjectsRoot, fs.Config.AutoInjectEnv, fs.Config.PathMapper, nil, nil, false)
	if loadErr != nil {
		project, loadErr = loadComposeProjectInternal(ctx, composeFile, normalizedName, fs.Config.ProjectsRoot, fs.Config.AutoInjectEnv, fs.Config.PathMapper, nil, nil, false)
		if loadErr != nil {
			return 0, nil, loadErr
		}
	}

	serviceCount = len(project.Services)
	if project.Name != "" && project.Name != normalizedName {
		composeProjectName = new(string)
		*composeProjectName = project.Name
	}

	return serviceCount, composeProjectName, nil
}

func (fs *ProjectFilesystem) StageCreate(workspace ProjectWorkspace, composeContent string, envContent *string) (*StagedProjectWorkspace, error) {
	target, err := fs.ResolveExisting(workspace)
	if err != nil {
		return nil, err
	}

	stagePath, err := fs.createStageDirectoryInternal()
	if err != nil {
		return nil, err
	}

	stageWorkspace := target
	stageWorkspace.Path = stagePath
	if err := WriteProjectFiles(fs.Config.ProjectsRoot, stageWorkspace.Path, composeContent, envContent); err != nil {
		_ = os.RemoveAll(stageWorkspace.Path)
		return nil, err
	}

	return &StagedProjectWorkspace{
		Current: target,
		Stage:   stageWorkspace,
		Target:  target,
	}, nil
}

func (fs *ProjectFilesystem) StageUpdate(workspace ProjectWorkspace, name *string, composeContent, envContent *string) (*StagedProjectWorkspace, error) {
	current, err := fs.ResolveExisting(workspace)
	if err != nil {
		return nil, err
	}

	target, err := fs.resolveTargetWorkspaceInternal(current, name)
	if err != nil {
		return nil, err
	}

	stagePath, err := fs.createStageDirectoryInternal()
	if err != nil {
		return nil, err
	}
	if err := CopyDirectoryContents(current.Path, stagePath); err != nil {
		_ = os.RemoveAll(stagePath)
		return nil, err
	}

	stageWorkspace := target
	stageWorkspace.Path = stagePath
	if err := fs.persistUpdatedProjectFilesInternal(stageWorkspace.Path, composeContent, envContent); err != nil {
		_ = os.RemoveAll(stageWorkspace.Path)
		return nil, err
	}

	return &StagedProjectWorkspace{
		Current: current,
		Stage:   stageWorkspace,
		Target:  target,
	}, nil
}

func (fs *ProjectFilesystem) StageGitSync(workspace ProjectWorkspace, composeContent string, gitEnvContent *string) (*StagedProjectWorkspace, error) {
	current, err := fs.ResolveExisting(workspace)
	if err != nil {
		return nil, err
	}

	stagePath, err := fs.createStageDirectoryInternal()
	if err != nil {
		return nil, err
	}
	if err := CopyDirectoryContents(current.Path, stagePath); err != nil {
		_ = os.RemoveAll(stagePath)
		return nil, err
	}

	stageWorkspace := current
	stageWorkspace.Path = stagePath
	if err := WriteComposeFile(fs.Config.ProjectsRoot, stageWorkspace.Path, composeContent); err != nil {
		_ = os.RemoveAll(stageWorkspace.Path)
		return nil, err
	}

	envUpdate, err := prepareGitSyncEnvUpdateInternal(stageWorkspace.Path, gitEnvContent)
	if err != nil {
		_ = os.RemoveAll(stageWorkspace.Path)
		return nil, err
	}
	if err := persistGitSyncEnvFilesInternal(stageWorkspace.Path, fs.Config.ProjectsRoot, envUpdate); err != nil {
		_ = os.RemoveAll(stageWorkspace.Path)
		return nil, err
	}

	return &StagedProjectWorkspace{
		Current: current,
		Stage:   stageWorkspace,
		Target:  current,
	}, nil
}

func (fs *ProjectFilesystem) Promote(stage *StagedProjectWorkspace) error {
	if fs == nil {
		return fmt.Errorf("project filesystem is nil")
	}
	if stage == nil {
		return fmt.Errorf("staged workspace is nil")
	}
	renamePath := fs.renamePath
	if renamePath == nil {
		renamePath = os.Rename
	}

	target, err := fs.ResolveExisting(stage.Target)
	if err != nil {
		return err
	}
	current, err := fs.ResolveExisting(stage.Current)
	if err != nil {
		return err
	}
	currentLivePath, targetLivePath, err := fs.resolvePromotionPathsInternal(current, target)
	if err != nil {
		return err
	}

	stagePath := filepath.Clean(stage.Stage.Path)
	backupPath, err := fs.reserveHiddenPathInternal(".arcane-project-backup-")
	if err != nil {
		return err
	}

	hadCurrent := false
	if _, statErr := os.Stat(currentLivePath); statErr == nil {
		hadCurrent = true
		if err := renamePath(currentLivePath, backupPath); err != nil {
			return fmt.Errorf("backup live project directory: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect live project directory: %w", statErr)
	}

	if err := renamePath(stagePath, targetLivePath); err != nil {
		if hadCurrent {
			if restoreErr := renamePath(backupPath, currentLivePath); restoreErr != nil {
				_ = os.RemoveAll(stagePath)
				return fmt.Errorf("promote staged project: %w; restore failed: %w", err, restoreErr)
			}
		}
		_ = os.RemoveAll(stagePath)
		return fmt.Errorf("promote staged project: %w", err)
	}

	if hadCurrent {
		_ = os.RemoveAll(backupPath)
	}

	return nil
}

func (fs *ProjectFilesystem) resolvePromotionPathsInternal(current, target ProjectWorkspace) (string, string, error) {
	currentPath := filepath.Clean(current.Path)
	targetPath := filepath.Clean(target.Path)

	if currentPath != targetPath {
		return currentPath, targetPath, nil
	}

	info, err := os.Lstat(currentPath)
	if err != nil {
		if os.IsNotExist(err) {
			return currentPath, targetPath, nil
		}
		return "", "", fmt.Errorf("inspect project workspace: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return currentPath, targetPath, nil
	}

	resolvedPath, err := filepath.EvalSymlinks(currentPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve project workspace symlink: %w", err)
	}
	resolvedPath = filepath.Clean(resolvedPath)

	return resolvedPath, resolvedPath, nil
}

func (fs *ProjectFilesystem) RemoveWorkspace(workspace ProjectWorkspace) error {
	workspace, err := fs.ResolveExisting(workspace)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(workspace.Path); err != nil {
		return fmt.Errorf("remove project workspace: %w", err)
	}

	return nil
}

func (fs *ProjectFilesystem) resolveTargetWorkspaceInternal(current ProjectWorkspace, name *string) (ProjectWorkspace, error) {
	target := current
	if name == nil {
		return target, nil
	}

	newName := strings.TrimSpace(*name)
	if newName == "" || newName == current.Name {
		return target, nil
	}

	dirName := SanitizeProjectName(newName)
	if dirName == "" || strings.Trim(dirName, "_") == "" {
		return ProjectWorkspace{}, fmt.Errorf("invalid project name: results in empty directory name")
	}

	target.Name = newName
	target.DirName = dirName
	target.Path = filepath.Join(fs.Config.ProjectsRoot, dirName)

	if current.Path != target.Path {
		if _, err := os.Stat(target.Path); err == nil {
			return ProjectWorkspace{}, fmt.Errorf("project directory already exists: %s", target.Path)
		} else if !os.IsNotExist(err) {
			return ProjectWorkspace{}, fmt.Errorf("failed to check project directory rename target: %w", err)
		}
	}

	return target, nil
}

func (fs *ProjectFilesystem) persistUpdatedProjectFilesInternal(stagePath string, composeContent, envContent *string) error {
	switch {
	case composeContent != nil:
		if err := WriteComposeFile(fs.Config.ProjectsRoot, stagePath, *composeContent); err != nil {
			return fmt.Errorf("failed to save project files: %w", err)
		}
		if envContent != nil {
			if err := persistEffectiveEnvContentInternal(stagePath, fs.Config.ProjectsRoot, *envContent); err != nil {
				return fmt.Errorf("failed to save project files: %w", err)
			}
		} else if err := ensureEffectiveEnvFileInternal(stagePath, fs.Config.ProjectsRoot); err != nil {
			return fmt.Errorf("failed to save project files: %w", err)
		}
	case envContent != nil:
		if err := persistEffectiveEnvContentInternal(stagePath, fs.Config.ProjectsRoot, *envContent); err != nil {
			return err
		}
	}

	return nil
}

func (fs *ProjectFilesystem) createStageDirectoryInternal() (string, error) {
	stagePath, err := os.MkdirTemp(fs.Config.ProjectsRoot, ".arcane-project-stage-*")
	if err != nil {
		return "", fmt.Errorf("create project staging directory: %w", err)
	}
	if err := os.Chmod(stagePath, 0o755); err != nil {
		_ = os.RemoveAll(stagePath)
		return "", fmt.Errorf("set project staging directory permissions: %w", err)
	}
	return stagePath, nil
}

func (fs *ProjectFilesystem) reserveHiddenPathInternal(prefix string) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		candidate := filepath.Join(fs.Config.ProjectsRoot, fmt.Sprintf("%s%d-%d", prefix, time.Now().UnixNano(), attempt))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}

	return "", fmt.Errorf("failed to reserve unique project filesystem path")
}

func resolveStoredEffectiveEnvContentInternal(state ProjectEnvState) (string, error) {
	if state.HasEffective {
		return state.EffectiveContent, nil
	}
	if state.HasGitSource || state.HasOverride {
		effectiveContent, err := BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
		if err != nil {
			return "", fmt.Errorf("build effective env content: %w", err)
		}
		return effectiveContent, nil
	}
	return state.DirectContent, nil
}

func persistEffectiveEnvContentInternal(projectPath, projectsDirectory, envContent string) error {
	state, err := ReadProjectEnvState(projectPath)
	if err != nil {
		return fmt.Errorf("read project env state: %w", err)
	}

	if !state.HasGitSource {
		if state.HasOverride {
			if err := RemoveProjectFile(projectsDirectory, projectPath, OverrideEnvFileName); err != nil {
				return err
			}
		}
		return WriteEnvFile(projectsDirectory, projectPath, envContent)
	}

	overrideContent, err := BuildOverrideEnvContent(state.GitContent, envContent)
	if err != nil {
		return fmt.Errorf("build override env content: %w", err)
	}

	effectiveContent, err := BuildEffectiveEnvContent(state.GitContent, overrideContent)
	if err != nil {
		return fmt.Errorf("build effective env content: %w", err)
	}

	if err := WriteEnvFile(projectsDirectory, projectPath, effectiveContent); err != nil {
		return err
	}

	if strings.TrimSpace(overrideContent) == "" {
		if err := RemoveProjectFile(projectsDirectory, projectPath, OverrideEnvFileName); err != nil {
			return err
		}
	} else if err := WriteProjectFile(projectsDirectory, projectPath, OverrideEnvFileName, overrideContent); err != nil {
		return err
	}

	return nil
}

func ensureEffectiveEnvFileInternal(projectPath, projectsDirectory string) error {
	state, err := ReadProjectEnvState(projectPath)
	if err != nil {
		return fmt.Errorf("read project env state: %w", err)
	}

	if !state.HasGitSource {
		if state.HasOverride {
			if err := RemoveProjectFile(projectsDirectory, projectPath, OverrideEnvFileName); err != nil {
				return err
			}
			effectiveContent, err := resolveStoredEffectiveEnvContentInternal(state)
			if err != nil {
				return err
			}
			return WriteEnvFile(projectsDirectory, projectPath, effectiveContent)
		}
		return EnsureEnvFile(projectsDirectory, projectPath)
	}

	effectiveContent, err := BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
	if err != nil {
		return fmt.Errorf("build effective env content: %w", err)
	}

	return WriteEnvFile(projectsDirectory, projectPath, effectiveContent)
}

type gitSyncEnvUpdateInternal struct {
	state            ProjectEnvState
	gitEnvContent    *string
	overrideContent  string
	effectiveContent *string
}

func prepareGitSyncEnvUpdateInternal(projectPath string, gitEnvContent *string) (gitSyncEnvUpdateInternal, error) {
	state, err := ReadProjectEnvState(projectPath)
	if err != nil {
		return gitSyncEnvUpdateInternal{}, fmt.Errorf("read project env state: %w", err)
	}

	update := gitSyncEnvUpdateInternal{
		state:         state,
		gitEnvContent: gitEnvContent,
	}

	if gitEnvContent == nil {
		effectiveContent, err := resolveStoredEffectiveEnvContentInternal(state)
		if err != nil {
			return gitSyncEnvUpdateInternal{}, err
		}
		if effectiveContent == "" && !state.HasEffective && !state.HasGitSource && !state.HasOverride {
			return update, nil
		}
		update.effectiveContent = &effectiveContent
		return update, nil
	}

	overrideContent, err := resolveOverrideContentForGitSyncInternal(state, *gitEnvContent)
	if err != nil {
		return gitSyncEnvUpdateInternal{}, err
	}
	update.overrideContent = overrideContent

	effectiveContent, err := BuildEffectiveEnvContent(*gitEnvContent, overrideContent)
	if err != nil {
		return gitSyncEnvUpdateInternal{}, fmt.Errorf("build effective env content: %w", err)
	}
	update.effectiveContent = &effectiveContent

	return update, nil
}

func resolveOverrideContentForGitSyncInternal(state ProjectEnvState, gitEnvContent string) (string, error) {
	switch {
	case state.HasGitSource:
		overrideContent, err := BuildOverrideEnvContent(state.GitContent, state.OverrideContent)
		if err != nil {
			return "", fmt.Errorf("build override env content: %w", err)
		}
		return overrideContent, nil
	case state.HasOverride:
		effectiveContent, err := resolveStoredEffectiveEnvContentInternal(state)
		if err != nil {
			return "", err
		}
		overrideContent, err := BuildOverrideEnvContent(gitEnvContent, effectiveContent)
		if err != nil {
			return "", fmt.Errorf("build override env content: %w", err)
		}
		return overrideContent, nil
	case strings.TrimSpace(state.DirectContent) != "":
		overrideContent, err := BuildAdditiveOverrideEnvContent(gitEnvContent, state.DirectContent)
		if err != nil {
			return "", fmt.Errorf("build override env content: %w", err)
		}
		return overrideContent, nil
	default:
		return "", nil
	}
}

func persistGitSyncEnvFilesInternal(projectPath, projectsDirectory string, update gitSyncEnvUpdateInternal) error {
	if update.gitEnvContent == nil {
		if update.state.HasGitSource {
			if err := RemoveProjectFile(projectsDirectory, projectPath, GitSourceEnvFileName); err != nil {
				return err
			}
		}
		if update.state.HasOverride {
			if err := RemoveProjectFile(projectsDirectory, projectPath, OverrideEnvFileName); err != nil {
				return err
			}
		}
		if update.effectiveContent != nil || update.state.HasEffective || update.state.HasGitSource || update.state.HasOverride {
			effectiveContent := ""
			if update.effectiveContent != nil {
				effectiveContent = *update.effectiveContent
			}
			return WriteEnvFile(projectsDirectory, projectPath, effectiveContent)
		}
		return EnsureEnvFile(projectsDirectory, projectPath)
	}

	if update.effectiveContent == nil {
		return fmt.Errorf("missing effective env content for git sync update")
	}

	if err := WriteEnvFile(projectsDirectory, projectPath, *update.effectiveContent); err != nil {
		return err
	}
	if err := WriteProjectFile(projectsDirectory, projectPath, GitSourceEnvFileName, *update.gitEnvContent); err != nil {
		return err
	}
	if strings.TrimSpace(update.overrideContent) == "" {
		if err := RemoveProjectFile(projectsDirectory, projectPath, OverrideEnvFileName); err != nil {
			return err
		}
	} else if err := WriteProjectFile(projectsDirectory, projectPath, OverrideEnvFileName, update.overrideContent); err != nil {
		return err
	}

	return nil
}
