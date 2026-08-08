package project

import (
	"log/slog"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

type gitSyncEnvUpdateInternal struct {
	state            projects.ProjectEnvState
	gitEnvContent    *string
	overrideContent  string
	effectiveContent *string
}

func (s *ProjectService) resolveEffectiveEnvContentForUpdateInternal(projectPath string, envContent *string) (*string, error) {
	if envContent != nil {
		return envContent, nil
	}

	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return nil, errors.WrapIf(err, "read project env state")
	}

	effectiveContent, err := resolveStoredEffectiveEnvContentInternal(state)
	if err != nil {
		return nil, err
	}
	if effectiveContent == "" && !state.HasEffective && !state.HasGitSource && !state.HasOverride {
		return nil, nil
	}

	return &effectiveContent, nil
}

func resolveStoredEffectiveEnvContentInternal(state projects.ProjectEnvState) (string, error) {
	if state.HasEffective {
		return state.EffectiveContent, nil
	}
	if state.HasGitSource || state.HasOverride {
		effectiveContent, err := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
		if err != nil {
			return "", errors.WrapIf(err, "build effective env content")
		}
		return effectiveContent, nil
	}
	return state.DirectContent, nil
}

func persistEffectiveEnvContentInternal(projectPath, projectsDirectory, envContent string) error {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return errors.WrapIf(err, "read project env state")
	}

	if state.HasGitSource && state.HasEffective && envContent == state.EffectiveContent {
		storedEffectiveContent, buildErr := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
		if buildErr == nil && envContent == storedEffectiveContent {
			return nil
		}
	}

	if !state.HasGitSource {
		if state.HasOverride {
			if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName); err != nil {
				return err
			}
		}
		return projects.WriteManagedEnvFile(projectsDirectory, projectPath, projects.EffectiveEnvFileName, state.EffectiveUnreadable, envContent)
	}

	overrideContent, err := projects.BuildOverrideEnvContent(state.GitContent, envContent)
	if err != nil {
		return errors.WrapIf(err, "build override env content")
	}

	effectiveContent, err := projects.BuildEffectiveEnvContent(state.GitContent, overrideContent)
	if err != nil {
		return errors.WrapIf(err, "build effective env content")
	}

	if err := projects.WriteManagedEnvFile(projectsDirectory, projectPath, projects.EffectiveEnvFileName, state.EffectiveUnreadable, effectiveContent); err != nil {
		return err
	}

	return projects.WriteManagedEnvFile(projectsDirectory, projectPath, projects.OverrideEnvFileName, state.OverrideUnreadable, overrideContent)
}

func (s *ProjectService) ensureEffectiveEnvFileInternal(projectPath, projectsDirectory string) error {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return errors.WrapIf(err, "read project env state")
	}

	if !state.HasGitSource {
		if state.HasOverride {
			if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName); err != nil {
				return err
			}
			effectiveContent, err := resolveStoredEffectiveEnvContentInternal(state)
			if err != nil {
				return err
			}
			return projects.WriteManagedEnvFile(projectsDirectory, projectPath, projects.EffectiveEnvFileName, state.EffectiveUnreadable, effectiveContent)
		}
		return projects.EnsureEnvFile(projectsDirectory, projectPath)
	}

	effectiveContent, err := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
	if err != nil {
		return errors.WrapIf(err, "build effective env content")
	}

	return projects.WriteManagedEnvFile(projectsDirectory, projectPath, projects.EffectiveEnvFileName, state.EffectiveUnreadable, effectiveContent)
}

func (s *ProjectService) prepareGitSyncEnvUpdateInternal(projectPath string, gitEnvContent *string) (gitSyncEnvUpdateInternal, error) {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return gitSyncEnvUpdateInternal{}, errors.WrapIf(err, "read project env state")
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

	overrideContent, err := s.resolveOverrideContentForGitSyncInternal(state, *gitEnvContent)
	if err != nil {
		return gitSyncEnvUpdateInternal{}, err
	}
	update.overrideContent = overrideContent

	effectiveContent, err := projects.BuildEffectiveEnvContent(*gitEnvContent, overrideContent)
	if err != nil {
		return gitSyncEnvUpdateInternal{}, errors.WrapIf(err, "build effective env content")
	}
	update.effectiveContent = &effectiveContent

	return update, nil
}

func (s *ProjectService) resolveOverrideContentForGitSyncInternal(state projects.ProjectEnvState, gitEnvContent string) (string, error) {
	switch {
	case state.HasGitSource:
		overrideContent, err := projects.BuildOverrideEnvContent(state.GitContent, state.OverrideContent)
		if err != nil {
			return "", errors.WrapIf(err, "build override env content")
		}
		return overrideContent, nil
	case state.HasOverride:
		effectiveContent, err := resolveStoredEffectiveEnvContentInternal(state)
		if err != nil {
			return "", err
		}
		overrideContent, err := projects.BuildOverrideEnvContent(gitEnvContent, effectiveContent)
		if err != nil {
			return "", errors.WrapIf(err, "build override env content")
		}
		return overrideContent, nil
	case strings.TrimSpace(state.DirectContent) != "":
		overrideContent, err := projects.BuildAdditiveOverrideEnvContent(gitEnvContent, state.DirectContent)
		if err != nil {
			return "", errors.WrapIf(err, "build override env content")
		}
		return overrideContent, nil
	default:
		return "", nil
	}
}

func persistGitSyncEnvFilesInternal(projectPath, projectsDirectory string, update gitSyncEnvUpdateInternal) error {
	if update.gitEnvContent == nil {
		if update.state.HasGitSource {
			if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.GitSourceEnvFileName); err != nil {
				return err
			}
		}
		if update.state.HasOverride {
			if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName); err != nil {
				return err
			}
		}
		if update.effectiveContent != nil || update.state.HasEffective || update.state.HasGitSource || update.state.HasOverride {
			effectiveContent := ""
			if update.effectiveContent != nil {
				effectiveContent = *update.effectiveContent
			}
			return projects.WriteManagedEnvFile(projectsDirectory, projectPath, projects.EffectiveEnvFileName, update.state.EffectiveUnreadable, effectiveContent)
		}
		if update.state.EffectiveUnreadable {
			slog.Warn("skipping permission-locked .env file; leaving it untouched", "projectPath", projectPath)
			return nil
		}
		return projects.EnsureEnvFile(projectsDirectory, projectPath)
	}

	if update.effectiveContent == nil {
		return errors.New("missing effective env content for git sync update")
	}

	if err := projects.WriteManagedEnvFile(projectsDirectory, projectPath, projects.EffectiveEnvFileName, update.state.EffectiveUnreadable, *update.effectiveContent); err != nil {
		return err
	}
	if err := projects.WriteManagedEnvFile(projectsDirectory, projectPath, projects.GitSourceEnvFileName, update.state.GitSourceUnreadable, *update.gitEnvContent); err != nil {
		return err
	}
	return projects.WriteManagedEnvFile(projectsDirectory, projectPath, projects.OverrideEnvFileName, update.state.OverrideUnreadable, update.overrideContent)
}

// ApplyGitSyncEnvToDirectory applies the same managed three-file environment
// merge used by single-file project syncs and returns the effective content
// before and after the update.
func (s *ProjectService) ApplyGitSyncEnvToDirectory(projectPath, projectsDirectory string, gitEnvContent *string) (before, after string, err error) {
	update, err := s.prepareGitSyncEnvUpdateInternal(projectPath, gitEnvContent)
	if err != nil {
		return "", "", errors.WrapIf(err, "failed to resolve git env state")
	}
	before = update.state.DirectContent
	if update.effectiveContent != nil {
		after = *update.effectiveContent
	}
	if err := persistGitSyncEnvFilesInternal(projectPath, projectsDirectory, update); err != nil {
		return "", "", errors.WrapIf(err, "failed to sync git env files")
	}
	return before, after, nil
}
