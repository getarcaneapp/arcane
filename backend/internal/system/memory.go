package system

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/moby/moby/client"
	"go.getarcane.app/sys/cgroup"
)

const dockerHostMemoryCacheTTL = 30 * time.Second

type dockerHostMemoryInfo struct {
	root  string
	total uint64
}

// GetDockerHostMemory reports enclosing guest memory when Docker shares its
// host's cgroup namespace. A false result leaves the caller's baseline intact.
func (s *SystemService) GetDockerHostMemory(ctx context.Context) (uint64, uint64, bool) {
	if runtime.GOOS != "linux" || s.dockerService == nil || s.dockerHostMemoryCache == nil || !cgroup.IsDockerContainer() {
		return 0, 0, false
	}

	info, found, err := s.dockerHostMemoryCache.GetWithLoaders(struct{}{}, func(_ []struct{}) (map[struct{}]dockerHostMemoryInfo, error) {
		metadata, err := s.loadDockerHostMemoryInternal(ctx)
		if err != nil {
			slog.DebugContext(ctx, "Docker host memory accounting unavailable", "error", err)
			// Cache failures too, without retaining expired metadata.
			metadata = dockerHostMemoryInfo{}
		}
		return map[struct{}]dockerHostMemoryInfo{{}: metadata}, nil
	})
	if err != nil || !found || info.total == 0 || ctx.Err() != nil {
		return 0, 0, false
	}

	used, err := cgroup.MemoryUsage(info.root)
	if err != nil {
		return 0, 0, false
	}
	return min(used, info.total), info.total, true
}

func (s *SystemService) loadDockerHostMemoryInternal(ctx context.Context) (dockerHostMemoryInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	containerID, err := cgroup.CurrentContainerID()
	if err != nil {
		return dockerHostMemoryInfo{}, err
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return dockerHostMemoryInfo{}, err
	}
	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return dockerHostMemoryInfo{}, err
	}
	if !strings.HasPrefix(inspect.Container.ID, containerID) || inspect.Container.HostConfig == nil ||
		inspect.Container.HostConfig.CgroupnsMode != "host" {
		return dockerHostMemoryInfo{}, errors.New("current container does not share the Docker host cgroup namespace")
	}
	// Inspection expands hostname and mountinfo short IDs before cgroup validation.
	root, err := cgroup.DockerHostRoot(inspect.Container.ID)
	if err != nil {
		return dockerHostMemoryInfo{}, err
	}

	info, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return dockerHostMemoryInfo{}, err
	}
	if info.Info.MemTotal <= 0 {
		return dockerHostMemoryInfo{}, errors.New("Docker host memory total is unavailable")
	}
	return dockerHostMemoryInfo{root: root, total: uint64(info.Info.MemTotal)}, nil
}
