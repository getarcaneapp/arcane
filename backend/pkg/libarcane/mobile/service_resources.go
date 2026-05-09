package mobile

import (
	"context"

	mobilepb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/mobile/v1"
)

// ---------- System ----------

func (s *MobileServer) GetDockerInfo(ctx context.Context, req *mobilepb.GetDockerInfoRequest) (*mobilepb.GetDockerInfoResponse, error) {
	if _, ok := UserIDFromContext(ctx); !ok {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if s.callbacks.DockerInfo == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.DockerInfo(ctx, req.GetEnvironmentId())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.GetDockerInfoResponse{InfoJson: data}, nil
}

func (s *MobileServer) GetAppVersion(ctx context.Context, _ *mobilepb.GetAppVersionRequest) (*mobilepb.GetAppVersionResponse, error) {
	if _, ok := UserIDFromContext(ctx); !ok {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if s.callbacks.AppVersion == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.AppVersion(ctx)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.GetAppVersionResponse{InfoJson: data}, nil
}

// ---------- Containers ----------

func (s *MobileServer) InspectContainer(ctx context.Context, req *mobilepb.InspectContainerRequest) (*mobilepb.InspectContainerResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.InspectContainer == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.InspectContainer(ctx, req.GetEnvironmentId(), req.GetId())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.InspectContainerResponse{DetailsJson: data}, nil
}

func (s *MobileServer) StartContainer(ctx context.Context, req *mobilepb.ContainerActionRequest) (*mobilepb.ActionResult, error) {
	return s.runContainerAction(ctx, req, s.callbacks.StartContainer)
}

func (s *MobileServer) StopContainer(ctx context.Context, req *mobilepb.ContainerActionRequest) (*mobilepb.ActionResult, error) {
	return s.runContainerAction(ctx, req, s.callbacks.StopContainer)
}

func (s *MobileServer) RestartContainer(ctx context.Context, req *mobilepb.ContainerActionRequest) (*mobilepb.ActionResult, error) {
	return s.runContainerAction(ctx, req, s.callbacks.RestartContainer)
}

func (s *MobileServer) RedeployContainer(ctx context.Context, req *mobilepb.ContainerActionRequest) (*mobilepb.ActionResult, error) {
	return s.runContainerAction(ctx, req, s.callbacks.RedeployContainer)
}

func (s *MobileServer) DeleteContainer(ctx context.Context, req *mobilepb.DeleteContainerRequest) (*mobilepb.ActionResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.DeleteContainer == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if err := s.callbacks.DeleteContainer(ctx, req.GetEnvironmentId(), req.GetId(), req.GetForce(), req.GetRemoveVolumes()); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}

func (s *MobileServer) PruneContainers(ctx context.Context, req *mobilepb.PruneContainersRequest) (*mobilepb.PruneResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.PruneContainers == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.PruneContainers(ctx, req.GetEnvironmentId())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.PruneResult{ReportJson: data}, nil
}

func (s *MobileServer) runContainerAction(ctx context.Context, req *mobilepb.ContainerActionRequest, fn SimpleEnvAction) (*mobilepb.ActionResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if err := fn(ctx, req.GetEnvironmentId(), req.GetId()); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}

// ---------- Volumes ----------

func (s *MobileServer) ListVolumes(ctx context.Context, req *mobilepb.ListVolumesRequest) (*mobilepb.ListVolumesResponse, error) {
	data, err := s.fetchEnvJSON(ctx, req.GetEnvironmentId(), s.callbacks.ListVolumes)
	if err != nil {
		return nil, err
	}
	return &mobilepb.ListVolumesResponse{VolumesJson: data}, nil
}

func (s *MobileServer) GetVolumeSizes(ctx context.Context, req *mobilepb.GetVolumeSizesRequest) (*mobilepb.GetVolumeSizesResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.GetVolumeSizes == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	sizes, err := s.callbacks.GetVolumeSizes(ctx, req.GetEnvironmentId())
	if err != nil {
		return nil, statusFromError(err)
	}
	out := make([]*mobilepb.VolumeSize, 0, len(sizes))
	for _, sz := range sizes {
		out = append(out, &mobilepb.VolumeSize{
			Name:      sz.Name,
			SizeBytes: sz.SizeBytes,
			RefCount:  sz.RefCount,
		})
	}
	return &mobilepb.GetVolumeSizesResponse{Sizes: out}, nil
}

func (s *MobileServer) CreateVolume(ctx context.Context, req *mobilepb.CreateVolumeRequest) (*mobilepb.CreateVolumeResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.CreateVolume == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.CreateVolume(ctx, req.GetEnvironmentId(), req.GetSpecJson())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.CreateVolumeResponse{VolumeJson: data}, nil
}

func (s *MobileServer) DeleteVolume(ctx context.Context, req *mobilepb.DeleteVolumeRequest) (*mobilepb.ActionResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.DeleteVolume == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if err := s.callbacks.DeleteVolume(ctx, req.GetEnvironmentId(), req.GetName(), req.GetForce()); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}

func (s *MobileServer) PruneVolumes(ctx context.Context, req *mobilepb.PruneVolumesRequest) (*mobilepb.PruneResult, error) {
	data, err := s.fetchEnvJSON(ctx, req.GetEnvironmentId(), s.callbacks.PruneVolumes)
	if err != nil {
		return nil, err
	}
	return &mobilepb.PruneResult{ReportJson: data}, nil
}

// ---------- Networks ----------

func (s *MobileServer) ListNetworks(ctx context.Context, req *mobilepb.ListNetworksRequest) (*mobilepb.ListNetworksResponse, error) {
	data, err := s.fetchEnvJSON(ctx, req.GetEnvironmentId(), s.callbacks.ListNetworks)
	if err != nil {
		return nil, err
	}
	return &mobilepb.ListNetworksResponse{NetworksJson: data}, nil
}

func (s *MobileServer) CreateNetwork(ctx context.Context, req *mobilepb.CreateNetworkRequest) (*mobilepb.CreateNetworkResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.CreateNetwork == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.CreateNetwork(ctx, req.GetEnvironmentId(), req.GetSpecJson())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.CreateNetworkResponse{NetworkJson: data}, nil
}

func (s *MobileServer) DeleteNetwork(ctx context.Context, req *mobilepb.DeleteNetworkRequest) (*mobilepb.ActionResult, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.DeleteNetwork == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if err := s.callbacks.DeleteNetwork(ctx, req.GetEnvironmentId(), req.GetId()); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.ActionResult{Success: true}, nil
}

func (s *MobileServer) PruneNetworks(ctx context.Context, req *mobilepb.PruneNetworksRequest) (*mobilepb.PruneResult, error) {
	data, err := s.fetchEnvJSON(ctx, req.GetEnvironmentId(), s.callbacks.PruneNetworks)
	if err != nil {
		return nil, err
	}
	return &mobilepb.PruneResult{ReportJson: data}, nil
}

// ---------- Projects (read) ----------

func (s *MobileServer) ListProjects(ctx context.Context, req *mobilepb.ListProjectsRequest) (*mobilepb.ListProjectsResponse, error) {
	data, err := s.fetchEnvJSON(ctx, req.GetEnvironmentId(), s.callbacks.ListProjects)
	if err != nil {
		return nil, err
	}
	return &mobilepb.ListProjectsResponse{ProjectsJson: data}, nil
}

func (s *MobileServer) GetProject(ctx context.Context, req *mobilepb.GetProjectRequest) (*mobilepb.GetProjectResponse, error) {
	if err := s.requireAuthAndLocalEnv(ctx, req.GetEnvironmentId()); err != nil {
		return nil, err
	}
	if s.callbacks.GetProject == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := s.callbacks.GetProject(ctx, req.GetEnvironmentId(), req.GetId())
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.GetProjectResponse{ProjectJson: data}, nil
}

// ---------- Helpers ----------

// requireAuthAndLocalEnv now only enforces authentication; remote-environment
// routing is handled by the callbacks in internal/api/grpc which proxy to the
// remote env via EnvironmentService.ExecuteRemoteRequest. The name is kept so
// the many existing call sites don't churn — semantically it's "requireAuth".
func (s *MobileServer) requireAuthAndLocalEnv(ctx context.Context, envID string) error {
	_ = envID
	if _, ok := UserIDFromContext(ctx); !ok {
		return statusFromError(ErrUnauthenticated)
	}
	return nil
}

func (s *MobileServer) fetchEnvJSON(ctx context.Context, envID string, fn JSONFetcher) ([]byte, error) {
	if err := s.requireAuthAndLocalEnv(ctx, envID); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	data, err := fn(ctx, envID)
	if err != nil {
		return nil, statusFromError(err)
	}
	return data, nil
}
