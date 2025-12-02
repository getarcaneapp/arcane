package builder

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/platformutil"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/grpcerrors"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
)

type Node struct {
	store.Node
	Builder     string
	Driver      *driver.DriverHandle
	DriverInfo  *driver.Info
	ImageOpt    imagetools.Opt
	ProxyConfig map[string]string
	Version     string
	Err         error

	// worker settings
	IDs        []string
	Platforms  []ocispecs.Platform
	GCPolicy   []client.PruneInfo
	Labels     map[string]string
	CDIDevices []client.CDIDevice
}

// Nodes returns nodes for this builder.
func (b *Builder) Nodes() []Node {
	return b.nodes
}

type LoadNodesOption func(*loadNodesOptions)

type loadNodesOptions struct {
	data      bool
	dialMeta  map[string][]string
	clientOpt []client.ClientOpt
}

func WithData() LoadNodesOption {
	return func(o *loadNodesOptions) {
		o.data = true
	}
}

func WithDialMeta(dialMeta map[string][]string) LoadNodesOption {
	return func(o *loadNodesOptions) {
		o.dialMeta = dialMeta
	}
}

func WithClientOpt(clientOpt ...client.ClientOpt) LoadNodesOption {
	return func(o *loadNodesOptions) {
		o.clientOpt = clientOpt
	}
}

// LoadNodes loads and returns nodes for this builder.
// TODO: this should be a method on a Node object and lazy load data for each driver.
func (b *Builder) LoadNodes(ctx context.Context, opts ...LoadNodesOption) (_ []Node, err error) {
	lno := loadNodesOptions{
		data: false,
	}
	for _, opt := range opts {
		opt(&lno)
	}

	eg, _ := errgroup.WithContext(ctx)
	b.nodes = make([]Node, len(b.NodeGroup.Nodes))

	defer func() {
		if b.err == nil && err != nil {
			b.err = err
		}
	}()

	factory, err := b.Factory(ctx, lno.dialMeta)
	if err != nil {
		return nil, err
	}

	imageopt, err := b.ImageOpt()
	if err != nil {
		return nil, err
	}

	for i, n := range b.NodeGroup.Nodes {
		func(i int, n store.Node) {
			eg.Go(func() error {
				node := Node{
					Node:        n,
					ProxyConfig: storeutil.GetProxyConfig(b.opts.dockerCli),
					Platforms:   n.Platforms,
					Builder:     b.Name,
				}
				defer func() {
					b.nodes[i] = node
				}()

				dockerapi, err := dockerutil.NewClientAPI(b.opts.dockerCli, n.Endpoint)
				if err != nil {
					node.Err = err
					return nil
				}

				d, err := driver.GetDriver(ctx, factory, driver.InitConfig{
					Name:            driver.BuilderName(n.Name),
					EndpointAddr:    n.Endpoint,
					DockerAPI:       dockerapi,
					DockerContext:   b.opts.dockerCli.CurrentContext(),
					ContextStore:    b.opts.dockerCli.ContextStore(),
					BuildkitdFlags:  n.BuildkitdFlags,
					Files:           n.Files,
					DriverOpts:      n.DriverOpts,
					Auth:            imageopt.Auth,
					Platforms:       n.Platforms,
					ContextPathHash: b.opts.contextPathHash,
					DialMeta:        lno.dialMeta,
				})
				if err != nil {
					node.Err = err
					return nil
				}
				node.Driver = d
				node.ImageOpt = imageopt

				if lno.data {
					if err := node.loadData(ctx, lno.clientOpt...); err != nil {
						node.Err = err
					}
				}
				return nil
			})
		}(i, n)
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// TODO: This should be done in the routine loading driver data
	if lno.data {
		kubernetesDriverCount := 0
		for _, d := range b.nodes {
			if d.DriverInfo != nil && len(d.DriverInfo.DynamicNodes) > 0 {
				kubernetesDriverCount++
			}
		}

		isAllKubernetesDrivers := len(b.nodes) == kubernetesDriverCount
		if isAllKubernetesDrivers {
			var nodes []Node
			var dynamicNodes []store.Node
			for _, di := range b.nodes {
				// dynamic nodes are used in Kubernetes driver.
				// Kubernetes' pods are dynamically mapped to BuildKit Nodes.
				if di.DriverInfo != nil && len(di.DriverInfo.DynamicNodes) > 0 {
					for i := range di.DriverInfo.DynamicNodes {
						diClone := di
						if pl := di.DriverInfo.DynamicNodes[i].Platforms; len(pl) > 0 {
							diClone.Platforms = pl
						}
						nodes = append(nodes, diClone)
					}
					dynamicNodes = append(dynamicNodes, di.DriverInfo.DynamicNodes...)
				}
			}

			// not append (remove the static nodes in the store)
			b.NodeGroup.Nodes = dynamicNodes
			b.nodes = nodes
			b.Dynamic = true
		}
	}

	return b.nodes, nil
}

func (n *Node) MarshalJSON() ([]byte, error) {
	var status string
	if n.DriverInfo != nil {
		status = n.DriverInfo.Status.String()
	}
	var nerr string
	if n.Err != nil {
		status = "error"
		nerr = strings.TrimSpace(n.Err.Error())
	}
	var pp []string
	for _, p := range n.Platforms {
		pp = append(pp, platforms.Format(p))
	}
	return json.Marshal(struct {
		Name           string
		Endpoint       string
		BuildkitdFlags []string           `json:"Flags,omitempty"`
		DriverOpts     map[string]string  `json:",omitempty"`
		Files          map[string][]byte  `json:",omitempty"`
		Status         string             `json:",omitempty"`
		ProxyConfig    map[string]string  `json:",omitempty"`
		Version        string             `json:",omitempty"`
		Err            string             `json:",omitempty"`
		IDs            []string           `json:",omitempty"`
		Platforms      []string           `json:",omitempty"`
		GCPolicy       []client.PruneInfo `json:",omitempty"`
		Labels         map[string]string  `json:",omitempty"`
	}{
		Name:           n.Name,
		Endpoint:       n.Endpoint,
		BuildkitdFlags: n.BuildkitdFlags,
		DriverOpts:     n.DriverOpts,
		Files:          n.Files,
		Status:         status,
		ProxyConfig:    n.ProxyConfig,
		Version:        n.Version,
		Err:            nerr,
		IDs:            n.IDs,
		Platforms:      pp,
		GCPolicy:       n.GCPolicy,
		Labels:         n.Labels,
	})
}

func (n *Node) loadData(ctx context.Context, clientOpt ...client.ClientOpt) error {
	if n.Driver == nil {
		return nil
	}
	info, err := n.Driver.Info(ctx)
	if err != nil {
		return err
	}
	n.DriverInfo = info
	if n.DriverInfo.Status == driver.Running {
		driverClient, err := n.Driver.Client(ctx, clientOpt...)
		if err != nil {
			return err
		}
		workers, err := driverClient.ListWorkers(ctx)
		if err != nil {
			return errors.Wrap(err, "listing workers")
		}
		for idx, w := range workers {
			n.IDs = append(n.IDs, w.ID)
			n.Platforms = append(n.Platforms, w.Platforms...)
			if idx == 0 {
				n.GCPolicy = w.GCPolicy
				n.Labels = w.Labels
			}
			n.CDIDevices = w.CDIDevices
		}
		sort.Strings(n.IDs)
		n.Platforms = platformutil.Dedupe(n.Platforms)
		inf, err := driverClient.Info(ctx)
		if err != nil {
			if st, ok := grpcerrors.AsGRPCStatus(err); ok && st.Code() == codes.Unimplemented {
				n.Version, err = n.Driver.Version(ctx)
				if err != nil {
					return errors.Wrap(err, "getting version")
				}
			}
		} else {
			n.Version = inf.BuildkitVersion.Version
		}
	}
	return nil
}
