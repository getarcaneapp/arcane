package docker

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

type Driver struct {
	factory driver.Factory
	driver.InitConfig

	// if you add fields, remember to update docs:
	// https://github.com/docker/docs/blob/main/content/build/drivers/docker.md
	features    features
	hostGateway hostGateway
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	return nil
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	_, err := d.DockerAPI.ServerVersion(ctx)
	if err != nil {
		return nil, errors.Wrap(driver.ErrNotConnecting{}, err.Error())
	}
	return &driver.Info{
		Status: driver.Running,
	}, nil
}

func (d *Driver) Version(ctx context.Context) (string, error) {
	v, err := d.DockerAPI.ServerVersion(ctx)
	if err != nil {
		return "", errors.Wrap(driver.ErrNotConnecting{}, err.Error())
	}
	if bkversion, _ := resolveBuildKitVersion(v.Version); bkversion != "" {
		return bkversion, nil
	}
	// https://github.com/moby/moby/blob/efc7a2abc3ab6dfa7d8d5d8c1c3b99138989b0f1/builder/builder-next/worker/worker.go#L176
	return strings.TrimSuffix(v.Version, "-moby"), nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	return nil
}

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	return nil
}

func (d *Driver) Dial(ctx context.Context) (net.Conn, error) {
	return d.DockerAPI.DialHijack(ctx, "/grpc", "h2c", d.DialMeta)
}

func (d *Driver) Client(ctx context.Context, opts ...client.ClientOpt) (*client.Client, error) {
	opts = append([]client.ClientOpt{
		client.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return d.Dial(ctx)
		}), client.WithSessionDialer(func(ctx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
			return d.DockerAPI.DialHijack(ctx, "/session", proto, meta)
		}),
	}, opts...)
	return client.New(ctx, "", opts...)
}

type features struct {
	once sync.Once
	list map[driver.Feature]bool
}

func (d *Driver) Features(ctx context.Context) map[driver.Feature]bool {
	d.features.once.Do(func() {
		var useContainerdSnapshotter bool
		if c, err := d.Client(ctx); err == nil {
			workers, _ := c.ListWorkers(ctx)
			for _, w := range workers {
				if _, ok := w.Labels["org.mobyproject.buildkit.worker.snapshotter"]; ok {
					useContainerdSnapshotter = true
				}
			}
			c.Close()
		}
		d.features.list = map[driver.Feature]bool{
			driver.OCIExporter:    useContainerdSnapshotter,
			driver.DockerExporter: useContainerdSnapshotter,
			driver.CacheExport:    useContainerdSnapshotter,
			driver.MultiPlatform:  useContainerdSnapshotter,
			driver.DirectPush:     useContainerdSnapshotter,
			driver.DefaultLoad:    true,
		}
	})
	return d.features.list
}

type hostGateway struct {
	once sync.Once
	ip   net.IP
	err  error
}

func (d *Driver) HostGatewayIP(ctx context.Context) (net.IP, error) {
	d.hostGateway.once.Do(func() {
		c, err := d.Client(ctx)
		if err != nil {
			d.hostGateway.err = err
			return
		}
		defer c.Close()
		workers, err := c.ListWorkers(ctx)
		if err != nil {
			d.hostGateway.err = errors.Wrap(err, "listing workers")
			return
		}
		for _, w := range workers {
			// should match github.com/docker/docker/builder/builder-next/worker/label.HostGatewayIP const
			if v, ok := w.Labels["org.mobyproject.buildkit.worker.moby.host-gateway-ip"]; ok && v != "" {
				ip := net.ParseIP(v)
				if ip == nil {
					d.hostGateway.err = errors.Errorf("failed to parse host-gateway IP: %s", v)
					return
				}
				d.hostGateway.ip = ip
				return
			}
		}
		d.hostGateway.err = errors.New("host-gateway IP not found")
	})
	return d.hostGateway.ip, d.hostGateway.err
}

func (d *Driver) Factory() driver.Factory {
	return d.factory
}

func (d *Driver) IsMobyDriver() bool {
	return true
}

func (d *Driver) Config() driver.InitConfig {
	return d.InitConfig
}
