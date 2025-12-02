package driver

import (
	"context"
	"sort"
	"sync"

	"github.com/docker/cli/cli/context/store"
	dockerclient "github.com/docker/docker/client"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/tracing/delegated"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type Factory interface {
	Name() string
	Usage() string
	Priority(ctx context.Context, endpoint string, api dockerclient.APIClient, dialMeta map[string][]string) int
	New(ctx context.Context, cfg InitConfig) (Driver, error)
	AllowsInstances() bool
}

type BuildkitConfig struct {
	// Entitlements []string
	// Rootless bool
}

type InitConfig struct {
	Name            string
	EndpointAddr    string
	DockerAPI       dockerclient.APIClient
	DockerContext   string
	ContextStore    store.Reader
	BuildkitdFlags  []string
	Files           map[string][]byte
	DriverOpts      map[string]string
	Auth            Auth
	Platforms       []ocispecs.Platform
	ContextPathHash string
	DialMeta        map[string][]string
}

var drivers map[string]Factory

func Register(f Factory) {
	if drivers == nil {
		drivers = map[string]Factory{}
	}
	drivers[f.Name()] = f
}

func GetDefaultFactory(ctx context.Context, ep string, c dockerclient.APIClient, instanceRequired bool, dialMeta map[string][]string) (Factory, error) {
	if len(drivers) == 0 {
		return nil, errors.Errorf("no drivers available")
	}
	type p struct {
		f        Factory
		priority int
	}
	dd := make([]p, 0, len(drivers))
	for _, f := range drivers {
		if instanceRequired && !f.AllowsInstances() {
			continue
		}
		dd = append(dd, p{f: f, priority: f.Priority(ctx, ep, c, dialMeta)})
	}
	sort.Slice(dd, func(i, j int) bool {
		return dd[i].priority < dd[j].priority
	})
	return dd[0].f, nil
}

func GetFactory(name string, instanceRequired bool) (Factory, error) {
	for _, f := range drivers {
		if f.Name() == name {
			if instanceRequired && !f.AllowsInstances() {
				return nil, errors.Errorf("additional instances of driver %q cannot be created", name)
			}
			return f, nil
		}
	}
	return nil, errors.Errorf("failed to find driver %q", name)
}

func GetDriver(ctx context.Context, f Factory, cfg InitConfig) (*DriverHandle, error) {
	if f == nil {
		var err error
		f, err = GetDefaultFactory(ctx, cfg.EndpointAddr, cfg.DockerAPI, false, cfg.DialMeta)
		if err != nil {
			return nil, err
		}
	}
	d, err := f.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &DriverHandle{Driver: d}, nil
}

func GetFactories(instanceRequired bool) []Factory {
	ds := make([]Factory, 0, len(drivers))
	for _, d := range drivers {
		if instanceRequired && !d.AllowsInstances() {
			continue
		}
		ds = append(ds, d)
	}
	sort.Slice(ds, func(i, j int) bool {
		return ds[i].Name() < ds[j].Name()
	})
	return ds
}

type DriverHandle struct {
	Driver
	client                  *client.Client
	err                     error
	once                    sync.Once
	historyAPISupportedOnce sync.Once
	historyAPISupported     bool
}

func (d *DriverHandle) Client(ctx context.Context, opt ...client.ClientOpt) (*client.Client, error) {
	d.once.Do(func() {
		d.client, d.err = d.Driver.Client(ctx, append(d.getClientOptions(), opt...)...)
	})
	return d.client, d.err
}

func (d *DriverHandle) getClientOptions() []client.ClientOpt {
	return []client.ClientOpt{
		client.WithTracerDelegate(delegated.DefaultExporter),
	}
}

func (d *DriverHandle) HistoryAPISupported(ctx context.Context) bool {
	d.historyAPISupportedOnce.Do(func() {
		if c, err := d.Client(ctx); err == nil {
			d.historyAPISupported = historyAPISupported(ctx, c)
		}
	})
	return d.historyAPISupported
}
