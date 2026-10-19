package pods

import (
	"context"
	"strings"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
)

type Service struct {
	client func(context.Context) (*podman.Client, error)
}

func NewService(client func(context.Context) (*podman.Client, error)) *Service {
	return &Service{client: client}
}

func (s *Service) List(ctx context.Context) ([]podmantypes.Pod, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.PodList(ctx, nil)
}

func (s *Service) Inspect(ctx context.Context, nameOrID string) (podmantypes.PodInspect, error) {
	client, err := s.client(ctx)
	if err != nil {
		return podmantypes.PodInspect{}, err
	}
	return client.PodInspect(ctx, nameOrID)
}

func (s *Service) Create(ctx context.Context, req podmantypes.PodCreateRequest) (string, error) {
	client, err := s.client(ctx)
	if err != nil {
		return "", err
	}

	portMappings := make([]podman.PodPortMapping, 0, len(req.PortMappings))
	for _, mapping := range req.PortMappings {
		portMappings = append(portMappings, podman.PodPortMapping{
			ContainerPort: mapping.ContainerPort,
			HostPort:      mapping.HostPort,
			HostIP:        mapping.HostIP,
			Protocol:      mapping.Protocol,
			Range:         mapping.Range,
		})
	}

	netNS, networks := func() (*podman.PodNamespace, map[string]podman.PodNetworkOptions) {
		switch network := strings.TrimSpace(req.Network); network {
		case "":
			return nil, nil
		case "bridge", "host", "none", "private", "slirp4netns", "pasta":
			return &podman.PodNamespace{NSMode: network}, nil
		default:
			return &podman.PodNamespace{NSMode: "bridge"}, map[string]podman.PodNetworkOptions{network: {}}
		}
	}()

	return client.PodCreate(ctx, podman.PodSpec{
		Name:             strings.TrimSpace(req.Name),
		Hostname:         strings.TrimSpace(req.Hostname),
		Labels:           req.Labels,
		NoInfra:          req.NoInfra,
		SharedNamespaces: req.SharedNamespaces,
		PortMappings:     portMappings,
		NetNS:            netNS,
		Networks:         networks,
		Advanced:         req.Advanced,
	})
}

func (s *Service) Action(ctx context.Context, nameOrID string, action podman.PodAction, req podmantypes.PodActionRequest) error {
	client, err := s.client(ctx)
	if err != nil {
		return err
	}
	return client.PodAction(ctx, nameOrID, action, podman.PodActionOptions{
		Timeout: req.Timeout,
		Signal:  req.Signal,
	})
}

func (s *Service) Remove(ctx context.Context, nameOrID string, force bool) error {
	client, err := s.client(ctx)
	if err != nil {
		return err
	}
	return client.PodRemove(ctx, nameOrID, force)
}

func (s *Service) Prune(ctx context.Context) ([]podmantypes.PodPruneReport, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.PodPrune(ctx)
}

func (s *Service) Top(ctx context.Context, nameOrID, psArgs string) (podmantypes.PodTop, error) {
	client, err := s.client(ctx)
	if err != nil {
		return podmantypes.PodTop{}, err
	}
	return client.PodTop(ctx, nameOrID, psArgs)
}

func (s *Service) Stats(ctx context.Context, namesOrIDs []string) ([]podmantypes.PodStats, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.PodStats(ctx, namesOrIDs)
}
