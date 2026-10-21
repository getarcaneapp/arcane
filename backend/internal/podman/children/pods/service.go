package pods

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/docker/go-units"
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

const cpuPeriod = 100000

type Service struct {
	runtime func(context.Context) (podman.Runtime, error)
}

func NewService(runtime func(context.Context) (podman.Runtime, error)) *Service {
	return &Service{runtime: runtime}
}

func (s *Service) List(ctx context.Context) ([]types.Pod, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return nil, err
	}
	return rt.Client.PodList(ctx, nil)
}

var podPagination = pagination.Config[types.Pod]{
	SearchAccessors: []pagination.SearchAccessor[types.Pod]{
		func(pod types.Pod) (string, error) {
			return pod.Name, nil
		},
		func(pod types.Pod) (string, error) {
			return pod.ID, nil
		},
		func(pod types.Pod) (string, error) {
			return pod.Status, nil
		},
		func(pod types.Pod) (string, error) {
			return strings.Join(pod.Networks, " "), nil
		},
	},
	SortBindings: []pagination.SortBinding[types.Pod]{
		{Key: "name", Fn: func(a, b types.Pod) int {
			return strings.Compare(a.Name, b.Name)
		}},
		{Key: "status", Fn: func(a, b types.Pod) int {
			return strings.Compare(a.Status, b.Status)
		}},
		{Key: "created", Fn: func(a, b types.Pod) int {
			return a.Created.Compare(b.Created)
		}},
	},
}

func (s *Service) ListPaginated(ctx context.Context, params pagination.QueryParams) ([]types.Pod, pagination.Response, error) {
	items, err := s.List(ctx)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	result := podPagination.SearchOrderAndPaginate(items, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *Service) Aggregate(ctx context.Context, params pagination.QueryParams) (types.Paginated[types.Pod], error) {
	collection, err := httpx.CollectEnvironments[types.Paginated[types.Pod]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return types.Paginated[types.Pod]{}, err
	}

	pages := make([]pagination.EnvironmentPage[types.Pod], 0, len(collection.Results))
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[types.Pod]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
	}
	data, resp := pagination.MergeEnvironmentPages(podPagination, pages, params)

	return types.Paginated[types.Pod]{
		Success:            true,
		Data:               data,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
}

func (s *Service) Inspect(ctx context.Context, nameOrID string) (types.PodInspect, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return types.PodInspect{}, err
	}
	pod, err := rt.Client.PodInspect(ctx, nameOrID)
	if err != nil {
		return types.PodInspect{}, err
	}
	containers, err := rt.Client.ContainerList(ctx, nil, []string{pod.ID})
	if err != nil {
		return types.PodInspect{}, fmt.Errorf("list containers for pod %s: %w", pod.ID, err)
	}

	for i, member := range pod.Containers {
		index := slices.IndexFunc(containers, func(c types.PodmanContainer) bool {
			return c.ID == member.ID
		})
		if index < 0 {
			continue
		}
		pod.Containers[i] = types.PodInspectContainer{
			ID:       member.ID,
			Name:     member.Name,
			State:    member.State,
			Image:    containers[index].Image,
			Status:   containers[index].Status,
			Created:  containers[index].Created,
			ExitCode: containers[index].ExitCode,
			IsInfra:  containers[index].IsInfra,
			Ports:    containers[index].Ports,
		}
	}
	return pod, nil
}

func (s *Service) Create(ctx context.Context, req types.PodCreateRequest) (string, error) {
	memory, err := byteSize("memory", req.Memory)
	if err != nil {
		return "", err
	}
	memorySwap, err := byteSize("memory swap", req.MemorySwap)
	if err != nil {
		return "", err
	}
	shmSize, err := byteSize("shm size", req.ShmSize)
	if err != nil {
		return "", err
	}
	shmSizeSystemd, err := byteSize("systemd shm size", req.ShmSizeSystemd)
	if err != nil {
		return "", err
	}
	weightDevices, err := deviceLimits("blkio weight", req.BlkioWeightDevices, func(major, minor int64, value string) (podman.PodWeightDevice, error) {
		weight, parseErr := strconv.ParseUint(value, 10, 16)
		return podman.PodWeightDevice{
			Major:  major,
			Minor:  minor,
			Weight: new(uint16(weight)),
		}, parseErr
	})
	if err != nil {
		return "", err
	}
	throttle := func(major, minor int64, value string) (podman.PodThrottleDevice, error) {
		rate, parseErr := units.RAMInBytes(value)
		return podman.PodThrottleDevice{
			Major: major,
			Minor: minor,
			Rate:  uint64(max(rate, 0)),
		}, parseErr
	}
	readBps, err := deviceLimits("read bps", req.DeviceReadBps, throttle)
	if err != nil {
		return "", err
	}
	writeBps, err := deviceLimits("write bps", req.DeviceWriteBps, throttle)
	if err != nil {
		return "", err
	}
	idMaps := [2][]podman.PodIDMap{}
	for i, entries := range [][]string{req.UIDMap, req.GIDMap} {
		for _, entry := range entries {
			var idMap podman.PodIDMap
			if _, scanErr := fmt.Sscanf(strings.TrimSpace(entry), "%d:%d:%d", &idMap.ContainerID, &idMap.HostID, &idMap.Size); scanErr != nil {
				return "", common.Classify(common.ErrBadRequest, fmt.Errorf("invalid ID mapping %q: use CONTAINER:HOST:SIZE", entry))
			}
			idMaps[i] = append(idMaps[i], idMap)
		}
	}

	var mounts []podman.PodMount
	var volumes []podman.PodNamedVolume
	for _, volume := range req.Volumes {
		if !strings.HasPrefix(volume.Source, "/") && strings.ContainsAny(volume.Source, `/\`) {
			return "", common.Classify(common.ErrBadRequest, fmt.Errorf("volume source %q must be an absolute host path or a named volume", volume.Source))
		}
		if strings.HasPrefix(volume.Source, "/") {
			mounts = append(mounts, podman.PodMount{
				Destination: volume.Destination,
				Type:        "bind",
				Source:      volume.Source,
				Options:     volume.Options,
			})
			continue
		}
		volumes = append(volumes, podman.PodNamedVolume{
			Name:    volume.Source,
			Dest:    volume.Destination,
			Options: volume.Options,
		})
	}

	rt, err := s.runtime(ctx)
	if err != nil {
		return "", err
	}

	netNS := namespace(cmp.Or(req.NetworkMode, kit.Ternary(len(req.Networks) > 0, "bridge", "")))
	return rt.Client.PodCreate(ctx, podman.PodSpec{
		Name:               strings.TrimSpace(req.Name),
		Hostname:           strings.TrimSpace(req.Hostname),
		ExitPolicy:         req.ExitPolicy,
		Labels:             req.Labels,
		NoInfra:            req.NoInfra,
		InfraConmonPidFile: req.InfraConmonPidFile,
		InfraCommand:       req.InfraCommand,
		InfraImage:         strings.TrimSpace(req.InfraImage),
		InfraName:          strings.TrimSpace(req.InfraName),
		Ipc:                namespace(req.Ipc),
		SharedNamespaces:   req.SharedNamespaces,
		RestartPolicy:      req.RestartPolicy,
		RestartRetries:     req.RestartRetries,
		ShareParent:        req.ShareParent,
		Pid:                namespace(req.Pid),
		Userns:             namespace(req.Userns),
		Uts:                namespace(req.Uts),
		Devices: func() []string {
			devices := slices.Clone(req.Devices)
			for _, gpu := range req.GPUs {
				devices = append(devices, "nvidia.com/gpu="+gpu)
			}
			return devices
		}(),
		Sysctl: req.Sysctl,
		NetNS:  netNS,
		PortMappings: func() []podman.PodPortMapping {
			out := make([]podman.PodPortMapping, 0, len(req.PortMappings))
			for _, mapping := range req.PortMappings {
				out = append(out, podman.PodPortMapping(mapping))
			}
			return out
		}(),
		Networks: func() map[string]podman.PodNetworkOptions {
			if len(req.Networks) == 0 {
				return nil
			}
			out := make(map[string]podman.PodNetworkOptions, len(req.Networks))
			for _, network := range req.Networks {
				out[strings.TrimSpace(network.Name)] = podman.PodNetworkOptions{
					StaticIPs:     network.StaticIPs,
					Aliases:       network.Aliases,
					StaticMAC:     network.StaticMAC,
					InterfaceName: network.InterfaceName,
				}
			}
			return out
		}(),
		NoManageResolvConf: req.NoManageResolvConf,
		DNSServer:          req.DNSServers,
		DNSSearch:          req.DNSSearch,
		DNSOption:          req.DNSOptions,
		NoManageHostname:   req.NoHostname,
		NoManageHosts:      req.NoHosts,
		HostAdd:            req.AddHosts,
		HostsFile:          req.HostsFile,
		NetworkOptions: func() map[string][]string {
			if len(req.NetworkOptions) == 0 || netNS == nil {
				return nil
			}
			return map[string][]string{netNS.NSMode: req.NetworkOptions}
		}(),
		CgroupParent: req.CgroupParent,
		ResourceLimits: &podman.PodResources{
			Memory: func() *podman.PodMemoryLimits {
				if memory == nil && memorySwap == nil {
					return nil
				}
				return &podman.PodMemoryLimits{Limit: memory, Swap: memorySwap}
			}(),
			CPU: func() *podman.PodCPULimits {
				if req.CPUs == 0 && req.CPUShares == 0 && req.CPUSetCPUs == "" && req.CPUSetMems == "" {
					return nil
				}
				return &podman.PodCPULimits{
					Shares: kit.Ternary(req.CPUShares > 0, new(req.CPUShares), nil),
					Quota:  kit.Ternary(req.CPUs > 0, new(int64(req.CPUs*cpuPeriod)), nil),
					Period: kit.Ternary(req.CPUs > 0, new(uint64(cpuPeriod)), nil),
					Cpus:   req.CPUSetCPUs,
					Mems:   req.CPUSetMems,
				}
			}(),
			BlockIO: func() *podman.PodBlockIO {
				if req.BlkioWeight == 0 && len(weightDevices)+len(readBps)+len(writeBps) == 0 {
					return nil
				}
				return &podman.PodBlockIO{
					Weight:                 kit.Ternary(req.BlkioWeight > 0, new(req.BlkioWeight), nil),
					WeightDevice:           weightDevices,
					ThrottleReadBpsDevice:  readBps,
					ThrottleWriteBpsDevice: writeBps,
				}
			}(),
		},
		Mounts:         mounts,
		Volumes:        volumes,
		VolumesFrom:    req.VolumesFrom,
		ShmSize:        shmSize,
		ShmSizeSystemd: shmSizeSystemd,
		SecurityOpt:    req.SecurityOpt,
		IDMappings: func() *podman.PodIDMappings {
			if len(idMaps[0])+len(idMaps[1]) == 0 {
				return nil
			}
			return &podman.PodIDMappings{
				UIDMap: kit.Ternary(len(idMaps[0]) > 0, idMaps[0], idMaps[1]),
				GIDMap: kit.Ternary(len(idMaps[1]) > 0, idMaps[1], idMaps[0]),
			}
		}(),
	})
}

func (s *Service) Action(ctx context.Context, nameOrID string, action podman.PodAction, req types.PodActionRequest) error {
	rt, err := s.runtime(ctx)
	if err != nil {
		return err
	}
	return rt.Client.PodAction(ctx, nameOrID, action, podman.PodActionOptions{
		Timeout: req.Timeout,
		Signal:  req.Signal,
	})
}

func (s *Service) Remove(ctx context.Context, nameOrID string, force bool) error {
	rt, err := s.runtime(ctx)
	if err != nil {
		return err
	}
	return rt.Client.PodRemove(ctx, nameOrID, force)
}

func (s *Service) Prune(ctx context.Context) ([]types.PodPruneReport, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return nil, err
	}
	return rt.Client.PodPrune(ctx)
}

func (s *Service) Top(ctx context.Context, nameOrID, psArgs string) (types.PodTop, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return types.PodTop{}, err
	}
	return rt.Client.PodTop(ctx, nameOrID, psArgs)
}

func (s *Service) Stats(ctx context.Context, namesOrIDs []string) ([]types.PodStats, error) {
	rt, err := s.runtime(ctx)
	if err != nil {
		return nil, err
	}
	return rt.Client.PodStats(ctx, namesOrIDs)
}
