package docker

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/buildx/driver"
	dockeropts "github.com/docker/cli/opts"
	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
)

const prioritySupported = 30
const priorityUnsupported = 70
const defaultRestartPolicy = "unless-stopped"

func init() {
	driver.Register(&factory{})
}

type factory struct {
}

func (*factory) Name() string {
	return "docker-container"
}

func (*factory) Usage() string {
	return "docker-container"
}

func (*factory) Priority(ctx context.Context, endpoint string, api dockerclient.APIClient, dialMeta map[string][]string) int {
	if api == nil {
		return priorityUnsupported
	}
	return prioritySupported
}

func (f *factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if cfg.DockerAPI == nil {
		return nil, errors.Errorf("%s driver requires docker API access", f.Name())
	}
	rp, err := dockeropts.ParseRestartPolicy(defaultRestartPolicy)
	if err != nil {
		return nil, err
	}
	d := &Driver{
		factory:       f,
		InitConfig:    cfg,
		restartPolicy: rp,
	}
	var gpus dockeropts.GpuOpts
	if err := gpus.Set("all"); err == nil {
		if v := gpus.Value(); len(v) > 0 {
			d.gpus = v
		}
	}
	for k, v := range cfg.DriverOpts {
		switch {
		case k == "network":
			d.netMode = v
		case k == "image":
			d.image = v
		case k == "memory":
			if err := d.memory.Set(v); err != nil {
				return nil, err
			}
		case k == "memory-swap":
			if err := d.memorySwap.Set(v); err != nil {
				return nil, err
			}
		case k == "cpu-period":
			vv, err := strconv.ParseInt(v, 10, 0)
			if err != nil {
				return nil, err
			}
			d.cpuPeriod = vv
		case k == "cpu-quota":
			vv, err := strconv.ParseInt(v, 10, 0)
			if err != nil {
				return nil, err
			}
			d.cpuQuota = vv
		case k == "cpu-shares":
			vv, err := strconv.ParseInt(v, 10, 0)
			if err != nil {
				return nil, err
			}
			d.cpuShares = vv
		case k == "cpuset-cpus":
			d.cpusetCpus = v
		case k == "cpuset-mems":
			d.cpusetMems = v
		case k == "cgroup-parent":
			d.cgroupParent = v
		case k == "restart-policy":
			d.restartPolicy, err = dockeropts.ParseRestartPolicy(v)
			if err != nil {
				return nil, err
			}
		case k == "default-load":
			d.defaultLoad, err = strconv.ParseBool(v)
			if err != nil {
				return nil, err
			}
		case strings.HasPrefix(k, "env."):
			envName := strings.TrimPrefix(k, "env.")
			if envName == "" {
				return nil, errors.Errorf("invalid env option %q, expecting env.FOO=bar", k)
			}
			d.env = append(d.env, fmt.Sprintf("%s=%s", envName, v))
		default:
			return nil, errors.Errorf("invalid driver option %s for docker-container driver", k)
		}
	}

	return d, nil
}

func (f *factory) AllowsInstances() bool {
	return true
}
