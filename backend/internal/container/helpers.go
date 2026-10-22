package container

import (
	"context"
	"errors"
	"slices"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

func (s *ContainerService) AuthorizeContainer(ctx context.Context, containerRef, perm string) (*container.InspectResponse, error) {
	if authz.ScopeFromContext(ctx, perm).IsUnrestricted() {
		return nil, nil
	}
	inspect, err := s.inspectAuthorizedContainer(ctx, containerRef, perm)
	if err != nil {
		return nil, err
	}
	return &inspect, nil
}

func (s *ContainerService) inspectAuthorizedContainer(ctx context.Context, containerRef, perm string) (container.InspectResponse, error) {
	result, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerRef, client.ContainerInspectOptions{})
	})
	if err != nil {
		return container.InspectResponse{}, err
	}
	attrs := authz.ContainerAttributes([]string{result.Container.Name}, kit.FromPtr(result.Container.Config).Labels)
	if scopeErr := recordScopeDenial(ctx, perm, authz.CheckResource(ctx, perm, authz.PermContainersRead, attrs)); scopeErr != nil {
		return container.InspectResponse{}, scopeErr
	}
	return result.Container, nil
}

func recordScopeDenial(ctx context.Context, perm string, err error) error {
	if errors.Is(err, authz.ErrResourceForbidden) {
		middleware.RecordDenial(ctx, types.AuthzDenial{
			Permission:    perm,
			EnvironmentID: authz.RequestEnvironmentFromContext(ctx),
			RemoteAddr:    middleware.GetRemoteAddrFromContext(ctx),
		})
	}
	return err
}

func containersInScope(ctx context.Context, perm string, containers []container.Summary) []container.Summary {
	scope := authz.ScopeFromContext(ctx, perm)
	if scope.IsUnrestricted() {
		return containers
	}
	return slices.DeleteFunc(slices.Clone(containers), func(dc container.Summary) bool {
		return !scope.Matches(authz.ContainerAttributes(dc.Names, dc.Labels))
	})
}
