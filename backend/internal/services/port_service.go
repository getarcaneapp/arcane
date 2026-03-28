package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	porttypes "github.com/getarcaneapp/arcane/types/port"
	"github.com/moby/moby/client"
)

type PortService struct {
	dockerService *DockerClientService
}

func NewPortService(dockerService *DockerClientService) *PortService {
	return &PortService{dockerService: dockerService}
}

func (s *PortService) ListPortsPaginated(ctx context.Context, params pagination.QueryParams) ([]porttypes.PortMapping, pagination.Response, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list containers: %w", err)
	}

	items := make([]porttypes.PortMapping, 0)
	for _, rawContainer := range containerList.Items {
		summary := containertypes.NewSummary(rawContainer)
		containerName := primaryContainerNameInternal(summary.Names, summary.ID)
		for _, port := range summary.Ports {
			items = append(items, porttypes.PortMapping{
				ID:            buildPortMappingIDInternal(summary.ID, port),
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

	result := pagination.SearchOrderAndPaginate(items, params, s.buildPortPaginationConfig())
	return result.Items, pagination.BuildResponseFromFilterResult(result, params), nil
}

func (s *PortService) buildPortPaginationConfig() pagination.Config[porttypes.PortMapping] {
	return pagination.Config[porttypes.PortMapping]{
		SearchAccessors: []pagination.SearchAccessor[porttypes.PortMapping]{
			func(item porttypes.PortMapping) (string, error) { return item.ContainerName, nil },
			func(item porttypes.PortMapping) (string, error) { return item.HostIP, nil },
			func(item porttypes.PortMapping) (string, error) { return strconv.Itoa(item.HostPort), nil },
			func(item porttypes.PortMapping) (string, error) { return strconv.Itoa(item.ContainerPort), nil },
			func(item porttypes.PortMapping) (string, error) { return item.Protocol, nil },
		},
		SortBindings: s.buildPortSortBindings(),
	}
}

func (s *PortService) buildPortSortBindings() []pagination.SortBinding[porttypes.PortMapping] {
	return []pagination.SortBinding[porttypes.PortMapping]{
		{
			Key: "hostPort",
			Fn: func(a, b porttypes.PortMapping) int {
				return compareOptionalIntInternal(a.HostPort, b.HostPort, a.ContainerName, b.ContainerName)
			},
			DescFn: func(a, b porttypes.PortMapping) int {
				return compareOptionalIntDescInternal(a.HostPort, b.HostPort, a.ContainerName, b.ContainerName)
			},
		},
		{
			Key: "containerPort",
			Fn: func(a, b porttypes.PortMapping) int {
				if a.ContainerPort != b.ContainerPort {
					if a.ContainerPort < b.ContainerPort {
						return -1
					}
					return 1
				}
				return strings.Compare(a.ContainerName, b.ContainerName)
			},
		},
		{
			Key: "protocol",
			Fn: func(a, b porttypes.PortMapping) int {
				if cmp := strings.Compare(a.Protocol, b.Protocol); cmp != 0 {
					return cmp
				}
				return strings.Compare(a.ContainerName, b.ContainerName)
			},
		},
		{
			Key: "containerName",
			Fn: func(a, b porttypes.PortMapping) int {
				return strings.Compare(a.ContainerName, b.ContainerName)
			},
		},
		{
			Key: "hostIp",
			Fn: func(a, b porttypes.PortMapping) int {
				return compareOptionalStringInternal(a.HostIP, b.HostIP, a.ContainerName, b.ContainerName)
			},
			DescFn: func(a, b porttypes.PortMapping) int {
				return compareOptionalStringDescInternal(a.HostIP, b.HostIP, a.ContainerName, b.ContainerName)
			},
		},
		{
			Key: "isPublished",
			Fn: func(a, b porttypes.PortMapping) int {
				if a.IsPublished != b.IsPublished {
					if a.IsPublished {
						return -1
					}
					return 1
				}
				return strings.Compare(a.ContainerName, b.ContainerName)
			},
		},
	}
}

func primaryContainerNameInternal(names []string, id string) string {
	if len(names) > 0 && names[0] != "" {
		return strings.TrimPrefix(names[0], "/")
	}
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}

func buildPortMappingIDInternal(containerID string, port containertypes.Port) string {
	return fmt.Sprintf("%s:%s:%d:%d:%s", containerID, port.IP, port.PublicPort, port.PrivatePort, port.Type)
}

func compareOptionalIntInternal(a, b int, fallbackA, fallbackB string) int {
	switch {
	case a == 0 && b == 0:
		return strings.Compare(fallbackA, fallbackB)
	case a == 0:
		return 1
	case b == 0:
		return -1
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return strings.Compare(fallbackA, fallbackB)
	}
}

func compareOptionalIntDescInternal(a, b int, fallbackA, fallbackB string) int {
	switch {
	case a == 0 && b == 0:
		return strings.Compare(fallbackA, fallbackB)
	case a == 0:
		return 1
	case b == 0:
		return -1
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return strings.Compare(fallbackA, fallbackB)
	}
}

func compareOptionalStringInternal(a, b, fallbackA, fallbackB string) int {
	a = normalizeOptionalStringSortValueInternal(a)
	b = normalizeOptionalStringSortValueInternal(b)

	switch {
	case a == "" && b == "":
		return strings.Compare(fallbackA, fallbackB)
	case a == "":
		return 1
	case b == "":
		return -1
	default:
		if cmp := strings.Compare(a, b); cmp != 0 {
			return cmp
		}
		return strings.Compare(fallbackA, fallbackB)
	}
}

func compareOptionalStringDescInternal(a, b, fallbackA, fallbackB string) int {
	a = normalizeOptionalStringSortValueInternal(a)
	b = normalizeOptionalStringSortValueInternal(b)

	switch {
	case a == "" && b == "":
		return strings.Compare(fallbackA, fallbackB)
	case a == "":
		return 1
	case b == "":
		return -1
	default:
		if cmp := strings.Compare(b, a); cmp != 0 {
			return cmp
		}
		return strings.Compare(fallbackA, fallbackB)
	}
}

func normalizeOptionalStringSortValueInternal(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "invalid IP") {
		return ""
	}
	return value
}
