package mobile

import (
	"context"

	mobilepb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/mobile/v1"
)

// GetServerInfo returns server-side build/runtime info. Used by iOS Dashboard
// "Server Info" tile.
func (s *MobileServer) GetServerInfo(ctx context.Context, _ *mobilepb.GetServerInfoRequest) (*mobilepb.GetServerInfoResponse, error) {
	if s.callbacks.ServerInfo == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}
	info, err := s.callbacks.ServerInfo(ctx)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.GetServerInfoResponse{
		ServerVersion:    info.ServerVersion,
		ServerRevision:   info.ServerRevision,
		DockerVersion:    info.DockerVersion,
		DockerApiVersion: info.DockerAPIVersion,
		Os:               info.OS,
		Arch:             info.Arch,
		EnvironmentCount: info.EnvironmentCount,
	}, nil
}

// ListContainers returns containers for the requested environment. Local
// envs go through svcs.Container.ListContainersPaginated; remote envs are
// proxied to the remote REST endpoint via EnvironmentService.ExecuteRemoteRequest
// inside the bound callback (internal/api/grpc/mobile_grpc_bootstrap.go).
func (s *MobileServer) ListContainers(ctx context.Context, req *mobilepb.ListContainersRequest) (*mobilepb.ListContainersResponse, error) {
	if s.callbacks.ListContainer == nil {
		return nil, statusFromError(ErrUnauthenticated)
	}

	envID := req.GetEnvironmentId()

	out, err := s.callbacks.ListContainer(ctx, ListContainersInput{
		EnvironmentID:   envID,
		IncludeAll:      req.GetIncludeAll(),
		IncludeInternal: req.GetIncludeInternal(),
		Search:          req.GetSearch(),
		Limit:           req.GetLimit(),
		Offset:          req.GetOffset(),
		GroupBy:         req.GetGroupBy(),
	})
	if err != nil {
		return nil, statusFromError(err)
	}

	return &mobilepb.ListContainersResponse{
		ContainersJson: out.ContainersJSON,
		Counts: &mobilepb.ContainerCounts{
			Running: out.Counts.Running,
			Stopped: out.Counts.Stopped,
			Paused:  out.Counts.Paused,
			Total:   out.Counts.Total,
		},
		Total: out.Total,
	}, nil
}

// GetCurrentDevice returns the device record for the calling token. Used by
// iOS on launch to verify a stored token is still valid.
func (s *MobileServer) GetCurrentDevice(ctx context.Context, _ *mobilepb.GetCurrentDeviceRequest) (*mobilepb.GetCurrentDeviceResponse, error) {
	deviceID, ok := DeviceIDFromContext(ctx)
	if !ok {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if s.callbacks.LookupDevice == nil {
		return nil, statusFromError(ErrDeviceNotFound)
	}

	device, err := s.callbacks.LookupDevice(ctx, deviceID)
	if err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.GetCurrentDeviceResponse{Device: deviceToProto(device)}, nil
}

// RevokeCurrentDevice revokes the calling device's token. The next gRPC call
// from the same client will fail with Unauthenticated.
func (s *MobileServer) RevokeCurrentDevice(ctx context.Context, _ *mobilepb.RevokeCurrentDeviceRequest) (*mobilepb.RevokeCurrentDeviceResponse, error) {
	deviceID, ok := DeviceIDFromContext(ctx)
	if !ok {
		return nil, statusFromError(ErrUnauthenticated)
	}
	if s.callbacks.RevokeDevice == nil {
		return nil, statusFromError(ErrDeviceNotFound)
	}
	if err := s.callbacks.RevokeDevice(ctx, deviceID); err != nil {
		return nil, statusFromError(err)
	}
	return &mobilepb.RevokeCurrentDeviceResponse{}, nil
}

// ---------- DTO ↔ proto conversion ----------

func deviceToProto(d Device) *mobilepb.Device {
	pb := &mobilepb.Device{
		Id:          d.ID,
		Name:        d.Name,
		DeviceId:    d.DeviceID,
		AppVersion:  d.AppVersion,
		OsVersion:   d.OsVersion,
		DeviceModel: d.DeviceModel,
	}
	if !d.PairedAt.IsZero() {
		pb.PairedAtUnixMs = d.PairedAt.UnixMilli()
	}
	if d.LastSeenAt != nil {
		pb.LastSeenAtUnixMs = d.LastSeenAt.UnixMilli()
	}
	return pb
}
