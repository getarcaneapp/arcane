package network

import (
	"context"
	"errors"
	"fmt"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

func recordNetworkDenial(ctx context.Context, perm string, err error) error {
	if errors.Is(err, authz.ErrResourceForbidden) {
		middleware.RecordDenial(ctx, types.AuthzDenial{
			Permission:    perm,
			EnvironmentID: authz.RequestEnvironmentFromContext(ctx),
			RemoteAddr:    middleware.GetRemoteAddrFromContext(ctx),
		})
	}
	return err
}

func authorizeNetworkAttributes(ctx context.Context, perm string, inspect network.Inspect) error {
	return recordNetworkDenial(ctx, perm, authz.CheckResource(ctx, perm, authz.PermNetworksRead, authz.NetworkAttributes(inspect.Name, inspect.Labels)))
}

func (s *NetworkService) authorizeNetwork(ctx context.Context, id, perm string) error {
	if authz.ScopeFromContext(ctx, perm).IsUnrestricted() {
		return nil
	}
	inspect, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NetworkInspectResult, error) {
		return compat.NetworkInspectWithCompatibility(ctx, dockerClient, id, client.NetworkInspectOptions{})
	})
	if err != nil {
		return common.ClassifyDockerError(fmt.Errorf("network not found: %w", err))
	}
	return authorizeNetworkAttributes(ctx, perm, inspect.Network)
}

func (s *NetworkService) authorizeNetworkAttachment(ctx context.Context, networkID, containerID, perm string) error {
	if authz.ScopeFromContext(ctx, perm).IsUnrestricted() {
		return nil
	}
	if err := s.authorizeNetwork(ctx, networkID, perm); err != nil {
		return err
	}
	inspect, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	})
	if err != nil {
		return common.ClassifyDockerError(fmt.Errorf("container not found: %w", err))
	}
	attrs := authz.ContainerAttributes([]string{inspect.Container.Name}, kit.FromPtr(inspect.Container.Config).Labels)
	return recordNetworkDenial(ctx, perm, authz.CheckResource(ctx, perm, authz.PermContainersRead, attrs))
}
