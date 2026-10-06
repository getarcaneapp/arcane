// Package sync manages the project env files a git sync writes: the git
// source, the Arcane override and the merged effective .env.
package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/project"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

func PersistEffectiveEnvContent(ctx context.Context, projectPath, projectsDirectory, envContent string) error {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return fmt.Errorf("read project env state: %w", err)
	}

	// WriteManagedEnvFile skips unreadable paths so git sync keeps working; an
	// explicit env save would then be dropped without any feedback.
	targets := []string{projects.EffectiveEnvFileName}
	if state.HasGitSource {
		targets = append(targets, projects.OverrideEnvFileName)
	}
	for _, name := range targets {
		if info, statErr := os.Stat(filepath.Join(projectPath, name)); statErr == nil && info.IsDir() {
			return fmt.Errorf("cannot save environment: %s is a directory", name)
		}
	}

	if state.HasGitSource && state.HasEffective && envContent == state.EffectiveContent {
		storedEffectiveContent, buildErr := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
		if buildErr == nil && envContent == storedEffectiveContent {
			return nil
		}
	}

	if !state.HasGitSource {
		if state.HasOverride {
			if removeProjectFileErr := projects.RemoveProjectFile(ctx, projectsDirectory, projectPath, projects.OverrideEnvFileName); removeProjectFileErr != nil {
				return removeProjectFileErr
			}
		}
		return projects.WriteManagedEnvFile(ctx, projectsDirectory, projectPath, projects.EffectiveEnvFileName, state.EffectiveUnreadable, envContent)
	}

	overrideContent, err := projects.BuildOverrideEnvContent(state.GitContent, envContent)
	if err != nil {
		return fmt.Errorf("build override env content: %w", err)
	}

	effectiveContent, err := projects.BuildEffectiveEnvContent(state.GitContent, overrideContent)
	if err != nil {
		return fmt.Errorf("build effective env content: %w", err)
	}

	if writeManagedEnvFileErr := projects.WriteManagedEnvFile(
		ctx,
		projectsDirectory,
		projectPath,
		projects.EffectiveEnvFileName,
		state.EffectiveUnreadable,
		effectiveContent,
	); writeManagedEnvFileErr != nil {
		return writeManagedEnvFileErr
	}

	return projects.WriteManagedEnvFile(ctx, projectsDirectory, projectPath, projects.OverrideEnvFileName, state.OverrideUnreadable, overrideContent)
}

func ResolveStoredEffectiveEnvContent(state projects.ProjectEnvState) (string, error) {
	if state.HasEffective {
		return state.EffectiveContent, nil
	}
	if state.HasGitSource || state.HasOverride {
		effectiveContent, err := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
		if err != nil {
			return "", fmt.Errorf("build effective env content: %w", err)
		}
		return effectiveContent, nil
	}
	return state.DirectContent, nil
}

// EffectiveEnvContentForUpdate returns the env content a project update keeps
// when the caller supplied none.
func EffectiveEnvContentForUpdate(projectPath string, envContent *string) (*string, error) {
	if envContent != nil {
		return envContent, nil
	}

	// A sync without git env content resolves exactly the stored effective content.
	update, err := PrepareGitSyncEnvUpdate(projectPath, nil)
	if err != nil {
		return nil, err
	}
	return update.EffectiveContent, nil
}

// EnsureEffectiveEnvFile rebuilds .env from the managed env sources.
func EnsureEffectiveEnvFile(ctx context.Context, projectPath, projectsDirectory string) error {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return fmt.Errorf("read project env state: %w", err)
	}

	if !state.HasGitSource {
		if state.HasOverride {
			if removeProjectFileErr := projects.RemoveProjectFile(ctx, projectsDirectory, projectPath, projects.OverrideEnvFileName); removeProjectFileErr != nil {
				return removeProjectFileErr
			}
			effectiveContent, resolveStoredEffectiveEnvContentErr := ResolveStoredEffectiveEnvContent(state)
			if resolveStoredEffectiveEnvContentErr != nil {
				return resolveStoredEffectiveEnvContentErr
			}
			return projects.WriteManagedEnvFile(ctx, projectsDirectory, projectPath, projects.EffectiveEnvFileName, state.EffectiveUnreadable, effectiveContent)
		}
		return projects.EnsureEnvFile(ctx, projectsDirectory, projectPath)
	}

	effectiveContent, err := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
	if err != nil {
		return fmt.Errorf("build effective env content: %w", err)
	}

	return projects.WriteManagedEnvFile(ctx, projectsDirectory, projectPath, projects.EffectiveEnvFileName, state.EffectiveUnreadable, effectiveContent)
}

// PrepareGitSyncEnvUpdate resolves the env merge for incoming git env content.
func PrepareGitSyncEnvUpdate(projectPath string, gitEnvContent *string) (GitSyncEnvUpdate, error) {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return GitSyncEnvUpdate{}, fmt.Errorf("read project env state: %w", err)
	}

	update := GitSyncEnvUpdate{
		State:         state,
		gitEnvContent: gitEnvContent,
	}

	if gitEnvContent == nil {
		effectiveContent, resolveStoredEffectiveEnvContentErr := ResolveStoredEffectiveEnvContent(state)
		if resolveStoredEffectiveEnvContentErr != nil {
			return GitSyncEnvUpdate{}, resolveStoredEffectiveEnvContentErr
		}
		if effectiveContent == "" && !state.HasEffective && !state.HasGitSource && !state.HasOverride {
			return update, nil
		}
		update.EffectiveContent = &effectiveContent
		return update, nil
	}

	switch {
	case state.HasGitSource:
		update.overrideContent, err = projects.BuildOverrideEnvContent(state.GitContent, state.OverrideContent)
	case state.HasOverride:
		storedContent, resolveErr := ResolveStoredEffectiveEnvContent(state)
		if resolveErr != nil {
			return GitSyncEnvUpdate{}, resolveErr
		}
		update.overrideContent, err = projects.BuildOverrideEnvContent(*gitEnvContent, storedContent)
	case strings.TrimSpace(state.DirectContent) != "":
		update.overrideContent, err = projects.BuildAdditiveOverrideEnvContent(*gitEnvContent, state.DirectContent)
	}
	if err != nil {
		return GitSyncEnvUpdate{}, fmt.Errorf("build override env content: %w", err)
	}

	effectiveContent, err := projects.BuildEffectiveEnvContent(*gitEnvContent, update.overrideContent)
	if err != nil {
		return GitSyncEnvUpdate{}, fmt.Errorf("build effective env content: %w", err)
	}
	update.EffectiveContent = &effectiveContent

	return update, nil
}

// GitSyncEnvUpdate is the resolved three-file env change for one git sync.
type GitSyncEnvUpdate struct {
	State            projects.ProjectEnvState
	gitEnvContent    *string
	overrideContent  string
	EffectiveContent *string
}

// PersistGitSyncEnvFiles writes a prepared env merge.
func PersistGitSyncEnvFiles(ctx context.Context, projectPath, projectsDirectory string, update GitSyncEnvUpdate) error {
	if update.gitEnvContent == nil {
		if update.State.HasGitSource {
			if err := projects.RemoveProjectFile(ctx, projectsDirectory, projectPath, projects.GitSourceEnvFileName); err != nil {
				return err
			}
		}
		if update.State.HasOverride {
			if err := projects.RemoveProjectFile(ctx, projectsDirectory, projectPath, projects.OverrideEnvFileName); err != nil {
				return err
			}
		}
		if update.EffectiveContent != nil || update.State.HasEffective || update.State.HasGitSource || update.State.HasOverride {
			return projects.WriteManagedEnvFile(ctx, projectsDirectory, projectPath, projects.EffectiveEnvFileName, update.State.EffectiveUnreadable, kit.FromPtr(update.EffectiveContent))
		}
		if update.State.EffectiveUnreadable {
			slog.WarnContext(ctx, "skipping permission-locked .env file; leaving it untouched", "projectPath", projectPath)
			return nil
		}
		return projects.EnsureEnvFile(ctx, projectsDirectory, projectPath)
	}

	if update.EffectiveContent == nil {
		return errors.New("missing effective env content for git sync update")
	}

	if err := projects.WriteManagedEnvFile(ctx, projectsDirectory, projectPath, projects.EffectiveEnvFileName, update.State.EffectiveUnreadable, *update.EffectiveContent); err != nil {
		return err
	}
	if err := projects.WriteManagedEnvFile(ctx, projectsDirectory, projectPath, projects.GitSourceEnvFileName, update.State.GitSourceUnreadable, *update.gitEnvContent); err != nil {
		return err
	}
	return projects.WriteManagedEnvFile(ctx, projectsDirectory, projectPath, projects.OverrideEnvFileName, update.State.OverrideUnreadable, update.overrideContent)
}

// EffectiveContent is the merged .env content, or nil when none is kept.

// HadGitSource reports whether the project tracked a git env source before.

// ApplyGitSyncEnv applies the managed three-file environment merge and returns
// the effective content before and after the update.
func ApplyGitSyncEnv(ctx context.Context, projectPath, projectsDirectory string, gitEnvContent *string) (before, after string, err error) {
	update, err := PrepareGitSyncEnvUpdate(projectPath, gitEnvContent)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve git env state: %w", err)
	}
	if persistErr := PersistGitSyncEnvFiles(ctx, projectPath, projectsDirectory, update); persistErr != nil {
		return "", "", fmt.Errorf("failed to sync git env files: %w", persistErr)
	}
	return update.State.DirectContent, kit.FromPtr(update.EffectiveContent), nil
}

// LoadComposeMetadata loads a discovered project's compose file once and
// returns the service count plus compose-go's effective project name.
func LoadComposeMetadata(ctx context.Context, dirPath, dirName, projectsDirectory string, autoInjectEnv bool, pathMapper *projects.PathMapper) (project.ComposeIdentity, error) {
	normName := projects.NormalizeProjectName(dirName)
	meta := project.ComposeIdentity{
		ResolvedProjectName: normName,
	}

	// Load unnamed first so COMPOSE_PROJECT_NAME from .env wins; fall back to
	// the normalized directory name when that fails.
	proj, _, err := projects.LoadComposeProjectFromDir(ctx, dirPath, "", projectsDirectory, autoInjectEnv, pathMapper)
	if err != nil {
		proj, _, err = projects.LoadComposeProjectFromDir(ctx, dirPath, normName, projectsDirectory, autoInjectEnv, pathMapper)
		if err != nil {
			return meta, err
		}
	} else if proj.Name != "" && proj.Name != normName {
		meta.ExplicitProjectName = true
	}

	meta.ServiceCount = len(proj.Services)
	if proj.Name != "" {
		meta.ResolvedProjectName = proj.Name
	}

	// Keep a COMPOSE_PROJECT_NAME override so containers match.
	if proj.Name != "" && proj.Name != normName {
		meta.ComposeProjectName = new(proj.Name)
	}

	return meta, nil
}
