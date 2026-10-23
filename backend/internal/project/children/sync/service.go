// Package sync manages the project env files a git sync writes (the git
// source, the Arcane override and the merged effective .env) and persists
// submitted project file updates.
package sync

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
)

func PersistEffectiveEnvContent(ctx context.Context, projectPath, projectsDirectory, envContent string) error {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return fmt.Errorf("read project env state: %w", err)
	}

	// WriteManagedEnvFile skips unreadable paths for git sync; an explicit env save would be dropped silently.
	for _, name := range kit.Ternary(
		state.HasGitSource,
		[]string{projects.EffectiveEnvFileName, projects.OverrideEnvFileName},
		[]string{projects.EffectiveEnvFileName},
	) {
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

// effectiveEnvContentForUpdate returns the env content a project update keeps
// when the caller supplied none.
func effectiveEnvContentForUpdate(projectPath string, envContent *string) (*string, error) {
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

// ensureEffectiveEnvFile rebuilds .env from the managed env sources.
func ensureEffectiveEnvFile(ctx context.Context, projectPath, projectsDirectory string) error {
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

// PersistUpdatedProjectFiles validates and writes submitted compose, override,
// and env content. An override-only save validates the on-disk base merged with
// the requested override, so a base only valid with its override still passes
// and a delete that would break the base fails before touching disk.
func PersistUpdatedProjectFiles(ctx context.Context, projectPath, projectName, projectsDirectory string, composeContent, envContent, overrideContent *string) error {
	if composeContent == nil && overrideContent == nil {
		if envContent != nil {
			return PersistEffectiveEnvContent(ctx, projectPath, projectsDirectory, *envContent)
		}
		return nil
	}

	baseContent, _, err := kit.Ternary(composeContent != nil, func() (string, string, error) {
		return *composeContent, "", nil
	}, func() (string, string, error) {
		return projects.ReadProjectFiles(ctx, projectPath, "")
	})()
	if err != nil {
		return fmt.Errorf("failed to read project files: %w", err)
	}
	effectiveEnvContent, err := effectiveEnvContentForUpdate(projectPath, envContent)
	if err != nil {
		return fmt.Errorf("invalid compose file: %w", err)
	}
	valOverride, valOverrideName := projects.ResolveEffectiveOverrideForValidation(projectPath, overrideContent)
	if validateErr := projects.ValidateComposeContentForUpdate(
		ctx, projectsDirectory, projectPath, projectName, baseContent, effectiveEnvContent, valOverride, valOverrideName, false,
	); validateErr != nil {
		return fmt.Errorf("invalid compose file: %w", validateErr)
	}

	// The env is persisted first so WriteComposeFile targets the COMPOSE_FILE
	// base the updated .env selects. A non-nil composeContent is an explicit
	// submission and is always written.
	if envContent != nil {
		if persistErr := PersistEffectiveEnvContent(ctx, projectPath, projectsDirectory, *envContent); persistErr != nil {
			return fmt.Errorf("failed to save project files: %w", persistErr)
		}
	} else if composeContent != nil {
		if ensureErr := ensureEffectiveEnvFile(ctx, projectPath, projectsDirectory); ensureErr != nil {
			return fmt.Errorf("failed to save project files: %w", ensureErr)
		}
	}
	if composeContent != nil {
		if writeErr := projects.WriteComposeFile(ctx, projectsDirectory, projectPath, *composeContent); writeErr != nil {
			return fmt.Errorf("failed to save project files: %w", writeErr)
		}
	}
	if overrideErr := projects.ApplyOverrideFileChange(ctx, projectsDirectory, projectPath, overrideContent); overrideErr != nil {
		return fmt.Errorf("failed to save project files: %w", overrideErr)
	}
	return nil
}

// PrepareGitSyncEnvUpdate resolves the env merge for incoming git env content.
func PrepareGitSyncEnvUpdate(projectPath string, gitEnvContent *string) (GitSyncEnvUpdate, error) {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return GitSyncEnvUpdate{}, fmt.Errorf("read project env state: %w", err)
	}

	if gitEnvContent == nil {
		effectiveContent, resolveStoredEffectiveEnvContentErr := ResolveStoredEffectiveEnvContent(state)
		if resolveStoredEffectiveEnvContentErr != nil {
			return GitSyncEnvUpdate{}, resolveStoredEffectiveEnvContentErr
		}
		return GitSyncEnvUpdate{
			State:            state,
			EffectiveContent: kit.Ternary(effectiveContent == "" && !state.HasEffective && !state.HasGitSource && !state.HasOverride, nil, &effectiveContent),
		}, nil
	}

	overrideContent, err := func() (string, error) {
		switch {
		case state.HasGitSource:
			content, buildErr := projects.BuildOverrideEnvContent(state.GitContent, state.OverrideContent)
			return content, kit.Ternary(buildErr == nil, nil, fmt.Errorf("build override env content: %w", buildErr))
		case state.HasOverride:
			storedContent, resolveErr := ResolveStoredEffectiveEnvContent(state)
			if resolveErr != nil {
				return "", resolveErr
			}
			content, buildErr := projects.BuildOverrideEnvContent(*gitEnvContent, storedContent)
			return content, kit.Ternary(buildErr == nil, nil, fmt.Errorf("build override env content: %w", buildErr))
		case strings.TrimSpace(state.DirectContent) != "":
			content, buildErr := projects.BuildAdditiveOverrideEnvContent(*gitEnvContent, state.DirectContent)
			return content, kit.Ternary(buildErr == nil, nil, fmt.Errorf("build override env content: %w", buildErr))
		}
		return "", nil
	}()
	if err != nil {
		return GitSyncEnvUpdate{}, err
	}

	effectiveContent, err := projects.BuildEffectiveEnvContent(*gitEnvContent, overrideContent)
	if err != nil {
		return GitSyncEnvUpdate{}, fmt.Errorf("build effective env content: %w", err)
	}
	return GitSyncEnvUpdate{
		State:            state,
		gitEnvContent:    gitEnvContent,
		overrideContent:  overrideContent,
		EffectiveContent: &effectiveContent,
	}, nil
}

// GitSyncEnvUpdate is the resolved three-file env change for one git sync.
type GitSyncEnvUpdate struct {
	State           projects.ProjectEnvState
	gitEnvContent   *string
	overrideContent string
	// EffectiveContent is the merged .env content, or nil when none is kept.
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

// LoadComposeMetadata returns a discovered project's service count and effective project name.
// Results are cached by dirPath until a compose, include or env file changes.
func LoadComposeMetadata(
	ctx context.Context,
	cache types.ComposeCache[types.ComposeIdentity],
	dirPath, dirName, projectsDirectory string,
	autoInjectEnv bool,
	pathMapper *projects.PathMapper,
) (types.ComposeIdentity, error) {
	normName := projects.NormalizeProjectName(dirName)
	composeFile, err := projects.DetectComposeFile(ctx, projectsDirectory, dirPath)
	if err != nil {
		return types.ComposeIdentity{ResolvedProjectName: normName}, err
	}
	fingerprint := fmt.Sprintf("%q|%q|%q|%t|%#v", composeFile, normName, projectsDirectory, autoInjectEnv, pathMapper)
	if cached, ok := cache.Get(dirPath, fingerprint); ok {
		return cached, nil
	}

	// Load unnamed first so COMPOSE_PROJECT_NAME from .env wins; fall back to
	// the normalized directory name when that fails.
	dependencies := types.ComposeDependencies{}
	loadedAt := time.Now()
	proj, loadErr := projects.LoadComposeProject(ctx, composeFile, "", projectsDirectory, autoInjectEnv, pathMapper, nil, nil, false, &dependencies, nil, nil)
	if loadErr != nil {
		dependencies = types.ComposeDependencies{}
		proj, err = projects.LoadComposeProject(ctx, composeFile, normName, projectsDirectory, autoInjectEnv, pathMapper, nil, nil, false, &dependencies, nil, nil)
		if err != nil {
			return types.ComposeIdentity{ResolvedProjectName: normName}, err
		}
	}
	meta := types.ComposeIdentity{
		ServiceCount:        len(proj.Services),
		ResolvedProjectName: cmp.Or(proj.Name, normName),
		// Keep a COMPOSE_PROJECT_NAME override so containers match.
		ComposeProjectName:  kit.Ternary(proj.Name != "" && proj.Name != normName, new(proj.Name), nil),
		ExplicitProjectName: loadErr == nil && proj.Name != "" && proj.Name != normName,
	}

	if composeFiles, envFiles, ok := projects.ComposeCacheDependencies(ctx, proj, dependencies, composeFile, projectsDirectory, autoInjectEnv); ok {
		if setErr := cache.Set(dirPath, fingerprint, dirPath, projectsDirectory, composeFile, composeFiles, envFiles, loadedAt, meta); setErr != nil {
			slog.DebugContext(ctx, "failed to cache compose metadata", "path", dirPath, "error", setErr)
		}
	}
	return meta, nil
}
