package port

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"

	"kit.getarcane.app"
	"kit.getarcane.app/docker"

	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	PortService struct {
		dockerService *dockerInternal.DockerClientService
	}
)

func NewPortService(dockerService *dockerInternal.DockerClientService) *PortService {
	return &PortService{dockerService: dockerService}
}

func (s *PortService) ListPortsPaginated(ctx context.Context, params pagination.QueryParams) ([]types.PortMapping, pagination.Response, error) {
	containers, err := s.dockerService.ListContainers(ctx, false)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list containers: %w", err)
	}

	items := make([]types.PortMapping, 0)
	for _, rawContainer := range containers {
		summary := types.NewContainerSummary(rawContainer)
		containerName := primaryContainerName(summary.Names, summary.ID)
		for _, port := range summary.Ports {
			items = append(items, types.PortMapping{
				ID:            fmt.Sprintf("%s:%s:%d:%d:%s", summary.ID, port.IP, port.PublicPort, port.PrivatePort, port.Type),
				ContainerID:   summary.ID,
				ContainerName: containerName,
				HostIP:        port.IP,
				HostPort:      port.PublicPort,
				ContainerPort: port.PrivatePort,
				Protocol:      port.Type,
				IsPublished:   port.PublicPort > 0,
			})
		}
	}

	result := s.buildPortPaginationConfig().SearchOrderAndPaginate(items, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *PortService) buildPortPaginationConfig() pagination.Config[types.PortMapping] {
	return pagination.Config[types.PortMapping]{
		SearchAccessors: []pagination.SearchAccessor[types.PortMapping]{
			func(item types.PortMapping) (string, error) { return item.ContainerName, nil },
			func(item types.PortMapping) (string, error) { return item.HostIP, nil },
			func(item types.PortMapping) (string, error) { return strconv.Itoa(item.HostPort), nil },
			func(item types.PortMapping) (string, error) { return strconv.Itoa(item.ContainerPort), nil },
			func(item types.PortMapping) (string, error) { return item.Protocol, nil },
		},
		SortBindings: s.buildPortSortBindings(),
	}
}

func (s *PortService) buildPortSortBindings() []pagination.SortBinding[types.PortMapping] {
	return []pagination.SortBinding[types.PortMapping]{
		{
			Key: "hostPort",
			Fn: func(a, b types.PortMapping) int {
				return cmp.Or(compareOptional(a.HostPort, b.HostPort, a.ContainerName, b.ContainerName, false), strings.Compare(a.ID, b.ID))
			},
			DescFn: func(a, b types.PortMapping) int {
				return cmp.Or(compareOptional(a.HostPort, b.HostPort, a.ContainerName, b.ContainerName, true), strings.Compare(a.ID, b.ID))
			},
		},
		{
			Key: "containerPort",
			Fn: func(a, b types.PortMapping) int {
				return cmp.Or(cmp.Compare(a.ContainerPort, b.ContainerPort), strings.Compare(a.ContainerName, b.ContainerName))
			},
		},
		{
			Key: "protocol",
			Fn: func(a, b types.PortMapping) int {
				return cmp.Or(strings.Compare(a.Protocol, b.Protocol), strings.Compare(a.ContainerName, b.ContainerName))
			},
		},
		{
			Key: "containerName",
			Fn: func(a, b types.PortMapping) int {
				return strings.Compare(a.ContainerName, b.ContainerName)
			},
		},
		{
			Key: "hostIp",
			Fn: func(a, b types.PortMapping) int {
				return compareOptional(a.HostIP, b.HostIP, a.ContainerName, b.ContainerName, false)
			},
			DescFn: func(a, b types.PortMapping) int {
				return compareOptional(a.HostIP, b.HostIP, a.ContainerName, b.ContainerName, true)
			},
		},
		{
			Key: "isPublished",
			Fn: func(a, b types.PortMapping) int {
				if a.IsPublished != b.IsPublished {
					return kit.Ternary(a.IsPublished, -1, 1)
				}
				return strings.Compare(a.ContainerName, b.ContainerName)
			},
		},
	}
}

func primaryContainerName(names []string, id string) string {
	if len(names) > 0 && names[0] != "" {
		return docker.ContainerNameFromNames(names)
	}
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}

func compareOptional[T cmp.Ordered](a, b T, fallbackA, fallbackB string, desc bool) int {
	var zero T
	switch {
	case a == zero && b == zero:
		return strings.Compare(fallbackA, fallbackB)
	case a == zero:
		return 1
	case b == zero:
		return -1
	}
	if desc {
		return cmp.Or(cmp.Compare(b, a), strings.Compare(fallbackA, fallbackB))
	}
	return cmp.Or(cmp.Compare(a, b), strings.Compare(fallbackA, fallbackB))
}
