package project

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	dockertypes "kit.getarcane.app/docker/types"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/secretsource"
	"getarcane.dev/types/v3"
)

// SecretEnvResolver supplies a project's bound secrets to container-creating
// operations. Implemented by secretsource.SecretSourceService.
type SecretEnvResolver interface {
	ResolveDeployEnv(ctx context.Context, project secretsource.ProjectRef, actor types.Actor) (secretsource.DeployEnv, error)
	RecordDeployed(ctx context.Context, projectID string, env secretsource.DeployEnv)
	DeployedKeys(ctx context.Context, projectID string) ([]string, error)
}

// SetSecretEnvResolver enables deploy-time secret injection.
func (s *ProjectService) SetSecretEnvResolver(resolver SecretEnvResolver) {
	s.secretEnv = resolver
}

// SecretProjectRef describes a project for secret bindings, including the
// paths needed to report which .env keys a secret overrides.
func (s *ProjectService) SecretProjectRef(ctx context.Context, projectID string) (secretsource.ProjectRef, error) {
	proj, found, err := s.FindProjectByID(ctx, projectID)
	if err != nil {
		return secretsource.ProjectRef{}, fmt.Errorf("failed to get project: %w", err)
	}
	if !found {
		return secretsource.ProjectRef{}, common.ErrProjectNotFound
	}
	projectsDir, dirErr := s.GetProjectsDirectory(ctx)
	if dirErr != nil {
		slog.DebugContext(ctx, "projects directory unavailable for secret override check", "error", dirErr)
		projectsDir = ""
	}
	return secretsource.ProjectRef{ID: proj.ID, Name: proj.Name, Path: proj.Path, ProjectsDirectory: projectsDir}, nil
}

// SecretKeysForComposeProject returns the names of the secrets the last
// deploy of a compose project delivered, so container views can mask their
// values. It returns nil when the project has no secret bindings or is not
// an Arcane project.
func (s *ProjectService) SecretKeysForComposeProject(ctx context.Context, composeName string) []string {
	if s.secretEnv == nil || composeName == "" {
		return nil
	}
	proj, err := s.GetProjectByComposeName(ctx, composeName)
	if err != nil || proj == nil {
		return nil
	}
	keys, err := s.secretEnv.DeployedKeys(ctx, proj.ID)
	if err != nil {
		slog.WarnContext(ctx, "failed to load deployed secret keys", "projectId", proj.ID, "error", err)
		return nil
	}
	return keys
}

func (s *ProjectService) resolveSecretEnvInternal(ctx context.Context, proj *Project, actor types.Actor) (secretsource.DeployEnv, error) {
	if s.secretEnv == nil || proj == nil {
		return secretsource.DeployEnv{}, nil
	}
	return s.secretEnv.ResolveDeployEnv(ctx, secretsource.ProjectRef{ID: proj.ID, Name: proj.Name}, actor)
}

// redactSecretOutputInternal masks the deploy's secret values in the progress
// stream and in the returned error. The returned function flushes the stream
// and masks the error; call it once the deploy is done.
func redactSecretOutputInternal(ctx context.Context, secretEnv secretsource.DeployEnv) (context.Context, func(error) error) {
	redactor := secretEnv.Redactor()
	if redactor == nil {
		return ctx, func(err error) error { return err }
	}
	var stream io.WriteCloser
	if progress, ok := ctx.Value(dockertypes.ProgressWriterKey{}).(io.Writer); ok && progress != nil {
		stream = redactor.Writer(progress)
		ctx = context.WithValue(ctx, dockertypes.ProgressWriterKey{}, io.Writer(stream))
	}
	return ctx, func(err error) error {
		if stream != nil {
			_ = stream.Close()
		}
		return redactor.Error(err)
	}
}

func (s *ProjectService) recordSecretDeployInternal(ctx context.Context, projectID string, secretEnv secretsource.DeployEnv) {
	if s.secretEnv == nil || secretEnv.Hash == "" {
		return
	}
	s.secretEnv.RecordDeployed(ctx, projectID, secretEnv)
}
