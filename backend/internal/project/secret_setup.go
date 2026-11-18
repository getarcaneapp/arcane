package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/secretsource"
	"getarcane.dev/arcane/v3/internal/secretsource/children/setupplan"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

// secretEnvBackupPrefix names .env backups written by secret setup.
const secretEnvBackupPrefix = ".env.before-secrets-"

// SecretSetupFiles returns the compose and env content that secret setup
// analyzes. Implements secretsource.ProjectFileAccess.
func (s *ProjectService) SecretSetupFiles(ctx context.Context, projectID string) (secretsource.ProjectFiles, error) {
	composeContent, envContent, overrideContent, err := s.projectContent(ctx, projectID)
	if err != nil {
		return secretsource.ProjectFiles{}, err
	}
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return secretsource.ProjectFiles{}, err
	}
	state, err := projects.ReadProjectEnvState(proj.Path)
	if err != nil {
		return secretsource.ProjectFiles{}, fmt.Errorf("read project env state: %w", err)
	}

	files := secretsource.ProjectFiles{
		EnvContent:    envContent,
		GitEnvContent: state.GitContent,
		HasGitSource:  state.HasGitSource,
	}
	for _, content := range []string{composeContent, overrideContent} {
		if strings.TrimSpace(content) != "" {
			files.ComposeContents = append(files.ComposeContents, content)
		}
	}
	return files, nil
}

// RemoveSecretEnvKeys removes keys that secret setup moved to a secret
// manager from the project's .env, through the regular project update so Git
// env layering, validation, and rollback apply. A key is removed only while
// its value still equals the verified one and no other entry references it.
// Implements secretsource.ProjectFileAccess.
func (s *ProjectService) RemoveSecretEnvKeys(ctx context.Context, projectID string, expected map[string]string, keepBackup bool, actor types.Actor) (secretsource.EnvRemoval, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return secretsource.EnvRemoval{}, err
	}
	if proj.IsArchived {
		return secretsource.EnvRemoval{}, common.ErrProjectArchived
	}
	state, err := projects.ReadProjectEnvState(proj.Path)
	if err != nil {
		return secretsource.EnvRemoval{}, fmt.Errorf("read project env state: %w", err)
	}
	if state.EffectiveUnreadable {
		return secretsource.EnvRemoval{}, errors.New("the project's .env is not readable")
	}
	// Parser errors quote nearby text, which can be a secret.
	current, err := projects.ParseProjectEnvContent(state.EffectiveContent, nil)
	if err != nil {
		return secretsource.EnvRemoval{}, errors.New("could not parse the project's .env; check its syntax")
	}

	result := secretsource.EnvRemoval{}
	keys := make([]string, 0, len(expected))
	for key, value := range expected {
		if currentValue, ok := current[key]; ok && currentValue == value {
			keys = append(keys, key)
		} else if ok {
			result.Changed = append(result.Changed, key)
		}
	}
	slices.Sort(result.Changed)

	updated, removed, referenced := setupplan.PlanEnvRemoval(state.EffectiveContent, keys)
	result.Removed, result.Referenced = removed, referenced
	if len(removed) == 0 {
		return result, nil
	}

	if keepBackup {
		backup, backupErr := s.writeSecretEnvBackupInternal(ctx, proj.Path, state.EffectiveContent)
		if backupErr != nil {
			return secretsource.EnvRemoval{}, backupErr
		}
		result.BackupFile = backup
	}

	if _, updateErr := s.UpdateProject(ctx, projectID, nil, nil, &updated, nil, actor); updateErr != nil {
		return secretsource.EnvRemoval{}, updateErr
	}
	return result, nil
}

// writeSecretEnvBackupInternal saves the .env before setup edits it. The
// backup still holds the secrets, so it must end up owner-only or not at all.
func (s *ProjectService) writeSecretEnvBackupInternal(ctx context.Context, projectPath, content string) (string, error) {
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve projects directory: %w", err)
	}
	name := secretEnvBackupPrefix + time.Now().UTC().Format("20060102-150405.000000000")
	if writeErr := projects.WriteProjectFile(ctx, projectsDirectory, projectPath, name, content); writeErr != nil {
		return "", fmt.Errorf("write .env backup: %w", writeErr)
	}
	path := filepath.Join(projectPath, name)
	if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
		if removeErr := os.Remove(path); removeErr != nil {
			slog.WarnContext(ctx, "could not remove .env backup after chmod failed", "path", path, "error", removeErr)
		}
		return "", fmt.Errorf("restrict .env backup permissions: %w", chmodErr)
	}
	return name, nil
}
