package builder

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/buildx/driver"
	k8sutil "github.com/docker/buildx/driver/kubernetes/util"
	remoteutil "github.com/docker/buildx/driver/remote/util"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	dopts "github.com/docker/cli/opts"
	"github.com/google/shlex"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.com/tonistiigi/go-csvvalue"
	"golang.org/x/sync/errgroup"
)

// Builder represents an active builder object
type Builder struct {
	*store.NodeGroup
	driverFactory driverFactory
	nodes         []Node
	opts          builderOpts
	err           error
}

type builderOpts struct {
	dockerCli       command.Cli
	name            string
	txn             *store.Txn
	contextPathHash string
	validate        bool
}

// Option provides a variadic option for configuring the builder.
type Option func(b *Builder)

// WithName sets builder name.
func WithName(name string) Option {
	return func(b *Builder) {
		b.opts.name = name
	}
}

// WithStore sets a store instance used at init.
func WithStore(txn *store.Txn) Option {
	return func(b *Builder) {
		b.opts.txn = txn
	}
}

// WithContextPathHash is used for determining pods in k8s driver instance.
func WithContextPathHash(contextPathHash string) Option {
	return func(b *Builder) {
		b.opts.contextPathHash = contextPathHash
	}
}

// WithSkippedValidation skips builder context validation.
func WithSkippedValidation() Option {
	return func(b *Builder) {
		b.opts.validate = false
	}
}

// New initializes a new builder client
func New(dockerCli command.Cli, opts ...Option) (_ *Builder, err error) {
	b := &Builder{
		opts: builderOpts{
			dockerCli: dockerCli,
			validate:  true,
		},
	}
	for _, opt := range opts {
		opt(b)
	}

	if b.opts.txn == nil {
		// if store instance is nil we create a short-lived one using the
		// default store and ensure we release it on completion
		var release func()
		b.opts.txn, release, err = storeutil.GetStore(dockerCli)
		if err != nil {
			return nil, err
		}
		defer release()
	}

	if b.opts.name != "" {
		if b.NodeGroup, err = storeutil.GetNodeGroup(b.opts.txn, dockerCli, b.opts.name); err != nil {
			return nil, err
		}
	} else {
		if b.NodeGroup, err = storeutil.GetCurrentInstance(b.opts.txn, dockerCli); err != nil {
			return nil, err
		}
	}
	if b.opts.validate {
		if err = b.Validate(); err != nil {
			return nil, err
		}
	}

	return b, nil
}

// Validate validates builder context
func (b *Builder) Validate() error {
	if b.NodeGroup != nil && b.DockerContext {
		list, err := b.opts.dockerCli.ContextStore().List()
		if err != nil {
			return err
		}
		currentContext := b.opts.dockerCli.CurrentContext()
		for _, l := range list {
			if l.Name == b.Name && l.Name != currentContext {
				return errors.Errorf("use `docker --context=%s buildx` to switch to context %q", l.Name, l.Name)
			}
		}
	}
	return nil
}

// ContextName returns builder context name if available.
func (b *Builder) ContextName() string {
	ctxbuilders, err := b.opts.dockerCli.ContextStore().List()
	if err != nil {
		return ""
	}
	for _, cb := range ctxbuilders {
		if b.Driver == "docker" && len(b.NodeGroup.Nodes) == 1 && b.NodeGroup.Nodes[0].Endpoint == cb.Name {
			return cb.Name
		}
	}
	return ""
}

// ImageOpt returns registry auth configuration
func (b *Builder) ImageOpt() (imagetools.Opt, error) {
	return storeutil.GetImageConfig(b.opts.dockerCli, b.NodeGroup)
}

// Boot bootstrap a builder
func (b *Builder) Boot(ctx context.Context) (bool, error) {
	toBoot := make([]int, 0, len(b.nodes))
	for idx, d := range b.nodes {
		if d.Err != nil || d.Driver == nil || d.DriverInfo == nil {
			continue
		}
		if d.DriverInfo.Status != driver.Running {
			toBoot = append(toBoot, idx)
		}
	}
	if len(toBoot) == 0 {
		return false, nil
	}

	printer, err := progress.NewPrinter(context.TODO(), os.Stderr, progressui.AutoMode)
	if err != nil {
		return false, err
	}

	baseCtx := ctx
	eg, _ := errgroup.WithContext(ctx)
	errCh := make(chan error, len(toBoot))
	for _, idx := range toBoot {
		func(idx int) {
			eg.Go(func() error {
				pw := progress.WithPrefix(printer, b.NodeGroup.Nodes[idx].Name, len(toBoot) > 1)
				_, err := driver.Boot(ctx, baseCtx, b.nodes[idx].Driver, pw)
				if err != nil {
					b.nodes[idx].Err = err
					errCh <- err
				}
				return nil
			})
		}(idx)
	}

	err = eg.Wait()
	close(errCh)
	err1 := printer.Wait()
	if err == nil {
		err = err1
	}

	if err == nil && len(errCh) > 0 {
		return false, <-errCh
	}
	return true, err
}

// Inactive checks if all nodes are inactive for this builder.
func (b *Builder) Inactive() bool {
	for _, d := range b.nodes {
		if d.DriverInfo != nil && d.DriverInfo.Status == driver.Running {
			return false
		}
	}
	return true
}

// Err returns error if any.
func (b *Builder) Err() error {
	return b.err
}

type driverFactory struct {
	driver.Factory
	once sync.Once
}

// Factory returns the driver factory.
func (b *Builder) Factory(ctx context.Context, dialMeta map[string][]string) (_ driver.Factory, err error) {
	b.driverFactory.once.Do(func() {
		if b.Driver != "" {
			b.driverFactory.Factory, err = driver.GetFactory(b.Driver, true)
			if err != nil {
				return
			}
		} else {
			// empty driver means nodegroup was implicitly created as a default
			// driver for a docker context and allows falling back to a
			// docker-container driver for older daemon that doesn't support
			// buildkit (< 18.06).
			ep := b.NodeGroup.Nodes[0].Endpoint
			var dockerapi *dockerutil.ClientAPI
			dockerapi, err = dockerutil.NewClientAPI(b.opts.dockerCli, b.NodeGroup.Nodes[0].Endpoint)
			if err != nil {
				return
			}
			// check if endpoint is healthy is needed to determine the driver type.
			// if this fails then can't continue with driver selection.
			if _, err = dockerapi.Ping(ctx); err != nil {
				return
			}
			b.driverFactory.Factory, err = driver.GetDefaultFactory(ctx, ep, dockerapi, false, dialMeta)
			if err != nil {
				return
			}
			b.Driver = b.driverFactory.Name()
		}
	})
	return b.driverFactory.Factory, err
}

func (b *Builder) MarshalJSON() ([]byte, error) {
	var berr string
	if b.err != nil {
		berr = strings.TrimSpace(b.err.Error())
	}
	return json.Marshal(struct {
		Name         string
		Driver       string
		LastActivity time.Time
		Dynamic      bool
		Nodes        []Node
		Err          string `json:",omitempty"`
	}{
		Name:         b.Name,
		Driver:       b.Driver,
		LastActivity: b.LastActivity,
		Dynamic:      b.Dynamic,
		Nodes:        b.nodes,
		Err:          berr,
	})
}

// GetBuilders returns all builders
func GetBuilders(dockerCli command.Cli, txn *store.Txn) ([]*Builder, error) {
	storeng, err := txn.List()
	if err != nil {
		return nil, err
	}

	contexts, err := dockerCli.ContextStore().List()
	if err != nil {
		return nil, err
	}
	sort.Slice(contexts, func(i, j int) bool {
		return contexts[i].Name < contexts[j].Name
	})

	builders := make([]*Builder, len(storeng), len(storeng)+len(contexts))
	seen := make(map[string]struct{})
	for i, ng := range storeng {
		b, err := New(dockerCli,
			WithName(ng.Name),
			WithStore(txn),
			WithSkippedValidation(),
		)
		if err != nil {
			return nil, err
		}
		builders[i] = b
		seen[b.Name] = struct{}{}
	}

	for _, c := range contexts {
		// if a context has the same name as an instance from the store, do not
		// add it to the builders list. An instance from the store takes
		// precedence over context builders.
		if _, ok := seen[c.Name]; ok {
			continue
		}
		b, err := New(dockerCli,
			WithName(c.Name),
			WithStore(txn),
			WithSkippedValidation(),
		)
		if err != nil {
			return nil, err
		}
		builders = append(builders, b)
	}

	return builders, nil
}

type CreateOpts struct {
	Name                string
	Driver              string
	NodeName            string
	Platforms           []string
	BuildkitdFlags      string
	BuildkitdConfigFile string
	DriverOpts          []string
	Use                 bool
	Endpoint            string
	Append              bool
}

func Create(ctx context.Context, txn *store.Txn, dockerCli command.Cli, opts CreateOpts) (*Builder, error) {
	var err error

	if opts.Name == "default" {
		return nil, errors.Errorf("default is a reserved name and cannot be used to identify builder instance")
	} else if opts.Append && opts.Name == "" {
		return nil, errors.Errorf("append requires a builder name")
	}

	name := opts.Name
	if name == "" {
		name, err = store.GenerateName(txn)
		if err != nil {
			return nil, err
		}
	}

	if !opts.Append {
		contexts, err := dockerCli.ContextStore().List()
		if err != nil {
			return nil, err
		}
		for _, c := range contexts {
			if c.Name == name {
				return nil, errors.Errorf("instance name %q already exists as context builder", name)
			}
		}
	}

	ng, err := txn.NodeGroupByName(name)
	if err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			if opts.Append && opts.Name != "" {
				return nil, errors.Errorf("failed to find instance %q for append", opts.Name)
			}
		} else {
			return nil, err
		}
	}

	buildkitHost := os.Getenv("BUILDKIT_HOST")

	driverName := opts.Driver
	if driverName == "" {
		if ng != nil {
			driverName = ng.Driver
		} else if opts.Endpoint == "" && buildkitHost != "" {
			driverName = "remote"
		} else {
			f, err := driver.GetDefaultFactory(ctx, opts.Endpoint, dockerCli.Client(), true, nil)
			if err != nil {
				return nil, err
			}
			if f == nil {
				return nil, errors.Errorf("no valid drivers found")
			}
			driverName = f.Name()
		}
	}

	if ng != nil {
		if opts.NodeName == "" && !opts.Append {
			return nil, errors.Errorf("existing instance for %q but no append mode, specify the node name to make changes for existing instances", name)
		}
		if driverName != ng.Driver {
			return nil, errors.Errorf("existing instance for %q but has mismatched driver %q", name, ng.Driver)
		}
	}

	if _, err := driver.GetFactory(driverName, true); err != nil {
		return nil, err
	}

	ngOriginal := ng
	if ngOriginal != nil {
		ngOriginal = ngOriginal.Copy()
	}

	if ng == nil {
		ng = &store.NodeGroup{
			Name:   name,
			Driver: driverName,
		}
	}

	driverOpts, err := csvToMap(opts.DriverOpts)
	if err != nil {
		return nil, err
	}

	buildkitdConfigFile := opts.BuildkitdConfigFile
	if buildkitdConfigFile == "" {
		// if buildkit daemon config is not provided, check if the default one
		// is available and use it
		if f, ok := confutil.NewConfig(dockerCli).BuildKitConfigFile(); ok {
			buildkitdConfigFile = f
		}
	}

	buildkitdFlags, err := parseBuildkitdFlags(opts.BuildkitdFlags, driverName, driverOpts, buildkitdConfigFile)
	if err != nil {
		return nil, err
	}

	var ep string
	var setEp bool
	switch {
	case driverName == "kubernetes":
		if opts.Endpoint != "" {
			return nil, errors.Errorf("kubernetes driver does not support endpoint args %q", opts.Endpoint)
		}
		// generate node name if not provided to avoid duplicated endpoint
		// error: https://github.com/docker/setup-buildx-action/issues/215
		nodeName := opts.NodeName
		if nodeName == "" {
			nodeName, err = k8sutil.GenerateNodeName(name, txn)
			if err != nil {
				return nil, err
			}
		}
		// naming endpoint to make append works
		ep = (&url.URL{
			Scheme: driverName,
			Path:   "/" + name,
			RawQuery: (&url.Values{
				"deployment": {nodeName},
				"kubeconfig": {os.Getenv("KUBECONFIG")},
			}).Encode(),
		}).String()
		setEp = false
	case driverName == "remote":
		if opts.Endpoint != "" {
			ep = opts.Endpoint
		} else if buildkitHost != "" {
			ep = buildkitHost
		} else {
			return nil, errors.Errorf("no remote endpoint provided")
		}
		ep, err = validateBuildkitEndpoint(ep)
		if err != nil {
			return nil, err
		}
		setEp = true
	case opts.Endpoint != "":
		ep, err = validateEndpoint(dockerCli, opts.Endpoint)
		if err != nil {
			return nil, err
		}
		setEp = true
	default:
		if dockerCli.CurrentContext() == "default" && dockerCli.DockerEndpoint().TLSData != nil {
			return nil, errors.Errorf("could not create a builder instance with TLS data loaded from environment. Please use `docker context create <context-name>` to create a context for current environment and then create a builder instance with context set to <context-name>")
		}
		ep, err = dockerutil.GetCurrentEndpoint(dockerCli)
		if err != nil {
			return nil, err
		}
		setEp = false
	}

	if err := ng.Update(opts.NodeName, ep, opts.Platforms, setEp, opts.Append, buildkitdFlags, buildkitdConfigFile, driverOpts); err != nil {
		return nil, err
	}

	if err := txn.Save(ng); err != nil {
		return nil, err
	}

	b, err := New(dockerCli,
		WithName(ng.Name),
		WithStore(txn),
		WithSkippedValidation(),
	)
	if err != nil {
		return nil, err
	}

	cancelCtx, cancel := context.WithCancelCause(ctx)
	timeoutCtx, _ := context.WithTimeoutCause(cancelCtx, 20*time.Second, errors.WithStack(context.DeadlineExceeded)) //nolint:govet // no need to manually cancel this context as we already rely on parent
	defer func() { cancel(errors.WithStack(context.Canceled)) }()

	nodes, err := b.LoadNodes(timeoutCtx, WithData())
	if err != nil {
		return nil, err
	}

	for _, node := range nodes {
		if err := node.Err; err != nil {
			err := errors.Errorf("failed to initialize builder %s (%s): %s", ng.Name, node.Name, err)
			var err2 error
			if ngOriginal == nil {
				err2 = txn.Remove(ng.Name)
			} else {
				err2 = txn.Save(ngOriginal)
			}
			if err2 != nil {
				return nil, errors.Errorf("could not rollback to previous state: %s", err2)
			}
			return nil, err
		}
	}

	if opts.Use && ep != "" {
		current, err := dockerutil.GetCurrentEndpoint(dockerCli)
		if err != nil {
			return nil, err
		}
		if err := txn.SetCurrent(current, ng.Name, false, false); err != nil {
			return nil, err
		}
	}

	return b, nil
}

type LeaveOpts struct {
	Name     string
	NodeName string
}

func Leave(ctx context.Context, txn *store.Txn, dockerCli command.Cli, opts LeaveOpts) error {
	if opts.Name == "" {
		return errors.Errorf("leave requires instance name")
	}
	if opts.NodeName == "" {
		return errors.Errorf("leave requires node name")
	}

	ng, err := txn.NodeGroupByName(opts.Name)
	if err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			return errors.Errorf("failed to find instance %q for leave", opts.Name)
		}
		return err
	}

	if err := ng.Leave(opts.NodeName); err != nil {
		return err
	}

	ls, err := localstate.New(confutil.NewConfig(dockerCli))
	if err != nil {
		return err
	}
	if err := ls.RemoveBuilderNode(ng.Name, opts.NodeName); err != nil {
		return err
	}

	return txn.Save(ng)
}

func csvToMap(in []string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	m := make(map[string]string, len(in))
	for _, s := range in {
		fields, err := csvvalue.Fields(s, nil)
		if err != nil {
			return nil, err
		}
		for _, v := range fields {
			p := strings.SplitN(v, "=", 2)
			if len(p) != 2 {
				return nil, errors.Errorf("invalid value %q, expecting k=v", v)
			}
			m[p[0]] = p[1]
		}
	}
	return m, nil
}

// validateEndpoint validates that endpoint is either a context or a docker host
func validateEndpoint(dockerCli command.Cli, ep string) (string, error) {
	dem, err := dockerutil.GetDockerEndpoint(dockerCli, ep)
	if err == nil && dem != nil {
		if ep == "default" {
			return dem.Host, nil
		}
		return ep, nil
	}
	h, err := dopts.ParseHost(true, ep)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse endpoint %s", ep)
	}
	return h, nil
}

// validateBuildkitEndpoint validates that endpoint is a valid buildkit host
func validateBuildkitEndpoint(ep string) (string, error) {
	if err := remoteutil.IsValidEndpoint(ep); err != nil {
		return "", err
	}
	return ep, nil
}

// parseBuildkitdFlags parses buildkit flags
func parseBuildkitdFlags(inp string, driver string, driverOpts map[string]string, buildkitdConfigFile string) (res []string, err error) {
	if inp != "" {
		res, err = shlex.Split(inp)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse buildkit flags")
		}
	}

	var allowInsecureEntitlements []string
	flags := pflag.NewFlagSet("buildkitd", pflag.ContinueOnError)
	flags.Usage = func() {}
	flags.StringArrayVar(&allowInsecureEntitlements, "allow-insecure-entitlement", nil, "")
	_ = flags.Parse(res)

	hasNetworkHostEntitlement := slices.Contains(allowInsecureEntitlements, "network.host")

	var hasNetworkHostEntitlementInConf bool
	if buildkitdConfigFile != "" {
		btoml, err := confutil.LoadConfigTree(buildkitdConfigFile)
		if err != nil {
			return nil, err
		} else if btoml != nil {
			if ies := btoml.GetArray("insecure-entitlements"); ies != nil {
				if slices.Contains(ies.([]string), "network.host") {
					hasNetworkHostEntitlementInConf = true
				}
			}
		}
	}

	if v, ok := driverOpts["network"]; ok && v == "host" && !hasNetworkHostEntitlement && driver == "docker-container" {
		// always set network.host entitlement if user has set network=host
		res = append(res, "--allow-insecure-entitlement=network.host")
	} else if len(allowInsecureEntitlements) == 0 && !hasNetworkHostEntitlementInConf && (driver == "kubernetes" || driver == "docker-container") {
		// set network.host entitlement if user does not provide any as
		// network is isolated for container drivers.
		res = append(res, "--allow-insecure-entitlement=network.host")
	}

	return res, nil
}
