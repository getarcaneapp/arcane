package project

import (
	"context"
	"errors"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/project/children/transfer"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	TransferDependencies struct {
		Transport        environment.RemoteTransport
		Tunnels          *agent.TunnelRegistry
		Backups          *backup.Engine
		RetargetWebhooks func(ctx context.Context, retarget types.ProjectWebhookRetarget) (int64, error)
		CountWebhooks    func(ctx context.Context, environmentID, projectID string) (int64, error)
		ExportVolume     func(ctx context.Context, volumeName string) (*types.UploadSession, error)
		RestoreVolume    func(ctx context.Context, volumeName string, request types.RestoreVolumeUploadRequest, user types.Actor) error
		DeleteVolume     func(ctx context.Context, name string, force bool, user types.Actor) error
	}

	localTransferEnvironment struct {
		projects *ProjectService
		deps     TransferDependencies
	}
)

func (e localTransferEnvironment) Availability(context.Context) transfer.Availability {
	return transfer.AvailabilityReady
}

func (e localTransferEnvironment) Project(ctx context.Context, projectID string) (types.Project, error) {
	return e.projects.GetProjectDetails(ctx, projectID, types.ProjectDetailsOptions{IncludeServiceConfigs: true})
}

func (e localTransferEnvironment) Projects(ctx context.Context, search string) ([]types.Project, error) {
	found, _, err := e.projects.ListProjects(ctx, pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: search},
		Params:      pagination.Params{Limit: -1},
	})
	return found, err
}

func (e localTransferEnvironment) VolumeLabels(ctx context.Context, name string) (map[string]string, bool, error) {
	dockerClient, err := e.projects.dockerService.GetClient(ctx)
	if err != nil {
		return nil, false, err
	}
	inspect, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return inspect.Volume.Labels, true, nil
}

func (e localTransferEnvironment) NetworkExists(ctx context.Context, name string) (bool, error) {
	dockerClient, err := e.projects.dockerService.GetClient(ctx)
	if err != nil {
		return false, err
	}
	_, err = dockerClient.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if errdefs.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (e localTransferEnvironment) ExportProject(ctx context.Context, projectID string) (types.UploadSession, error) {
	session, err := e.projects.ExportProjectArchive(ctx, projectID)
	if err != nil {
		return types.UploadSession{}, err
	}
	return *session, nil
}

func (e localTransferEnvironment) ImportProject(ctx context.Context, request types.ImportProjectArchive, user types.Actor) (string, error) {
	imported, err := e.projects.ImportProjectArchive(ctx, request, user)
	if err != nil {
		return "", err
	}
	return imported.ID, nil
}

func (e localTransferEnvironment) CreateUpload(ctx context.Context, kind string, request types.CreateUploadSessionRequest) (types.UploadSession, error) {
	session, err := e.projects.uploads.CreateSession(ctx, kind, request)
	if err != nil {
		return types.UploadSession{}, err
	}
	return *session, nil
}

func (e localTransferEnvironment) Upload(ctx context.Context, kind, uploadID string) (types.UploadSession, error) {
	session, err := e.projects.uploads.GetSession(ctx, kind, uploadID)
	if err != nil {
		return types.UploadSession{}, err
	}
	return *session, nil
}

func (e localTransferEnvironment) ReadChunk(ctx context.Context, kind, uploadID string, index int) ([]byte, error) {
	return e.projects.uploads.ReadChunk(ctx, kind, uploadID, index)
}

func (e localTransferEnvironment) WriteChunk(ctx context.Context, kind, uploadID string, index int, data []byte) error {
	_, err := e.projects.uploads.WriteChunk(ctx, kind, uploadID, index, data)
	return err
}

func (e localTransferEnvironment) DeleteUpload(ctx context.Context, kind, uploadID string) error {
	err := e.projects.uploads.DeleteSession(ctx, kind, uploadID)
	if errors.Is(err, common.ErrUploadSessionNotFound) {
		return nil
	}
	return err
}

func (e localTransferEnvironment) ExportVolume(ctx context.Context, name string) (types.UploadSession, error) {
	session, err := e.deps.ExportVolume(ctx, name)
	if err != nil {
		return types.UploadSession{}, err
	}
	return *session, nil
}

func (e localTransferEnvironment) RestoreVolume(ctx context.Context, name string, request types.RestoreVolumeUploadRequest, user types.Actor) error {
	return e.deps.RestoreVolume(ctx, name, request, user)
}

func (e localTransferEnvironment) DeployProject(ctx context.Context, projectID string, user types.Actor) error {
	return e.projects.DeployProject(ctx, projectID, user, nil)
}

func (e localTransferEnvironment) DownProject(ctx context.Context, projectID string, user types.Actor) error {
	return e.projects.DownProject(ctx, projectID, user)
}

func (e localTransferEnvironment) DestroyProject(ctx context.Context, projectID string, user types.Actor) error {
	return e.projects.DestroyProject(ctx, projectID, DestroyOptions{RemoveFiles: true}, user)
}

func (e localTransferEnvironment) DeleteVolume(ctx context.Context, name string, user types.Actor) error {
	err := e.deps.DeleteVolume(ctx, name, false, user)
	if errdefs.IsNotFound(err) {
		return nil
	}
	return err
}
