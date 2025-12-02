package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/docker/buildx/driver"
	util "github.com/docker/buildx/driver/remote/util"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/connhelper"
	"github.com/moby/buildkit/util/tracing/delegated"
	"github.com/pkg/errors"
)

type Driver struct {
	factory driver.Factory
	driver.InitConfig

	// if you add fields, remember to update docs:
	// https://github.com/docker/docs/blob/main/content/build/drivers/remote.md
	*tlsOpts
	defaultLoad bool

	// remote driver caches the client because its Bootstrap/Info methods reuse it internally
	clientOnce sync.Once
	client     *client.Client
	err        error
}

type tlsOpts struct {
	serverName string
	caCert     string
	cert       string
	key        string
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	c, err := d.Client(ctx)
	if err != nil {
		return err
	}
	return progress.Wrap("[internal] waiting for connection", l, func(_ progress.SubLogger) error {
		cancelCtx, cancel := context.WithCancelCause(ctx)
		ctx, _ := context.WithTimeoutCause(cancelCtx, 20*time.Second, errors.WithStack(context.DeadlineExceeded)) //nolint:govet // no need to manually cancel this context as we already rely on parent
		defer func() { cancel(errors.WithStack(context.Canceled)) }()
		return c.Wait(ctx)
	})
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	c, err := d.Client(ctx)
	if err != nil {
		return &driver.Info{
			Status: driver.Inactive,
		}, nil
	}

	if _, err := c.ListWorkers(ctx); err != nil {
		return &driver.Info{
			Status: driver.Inactive,
		}, nil
	}

	return &driver.Info{
		Status: driver.Running,
	}, nil
}

func (d *Driver) Version(ctx context.Context) (string, error) {
	return "", nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	return nil
}

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	return nil
}

func (d *Driver) Client(ctx context.Context, opts ...client.ClientOpt) (*client.Client, error) {
	d.clientOnce.Do(func() {
		opts = append([]client.ClientOpt{
			client.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return d.Dial(ctx)
			}),
			client.WithTracerDelegate(delegated.DefaultExporter),
		}, opts...)
		c, err := client.New(ctx, "", opts...)
		d.client = c
		d.err = err
	})
	return d.client, d.err
}

func (d *Driver) Dial(ctx context.Context) (net.Conn, error) {
	addr := d.EndpointAddr
	ch, err := connhelper.GetConnectionHelper(addr)
	if err != nil {
		return nil, err
	}
	if ch != nil {
		return ch.ContextDialer(ctx, addr)
	}

	network, addr, ok := strings.Cut(addr, "://")
	if !ok {
		return nil, errors.Errorf("invalid endpoint address: %s", d.EndpointAddr)
	}

	conn, err := util.DialContext(ctx, network, addr)

	if err != nil {
		return nil, errors.WithStack(err)
	}

	if d.tlsOpts != nil {
		cfg, err := loadTLS(d.tlsOpts)
		if err != nil {
			return nil, errors.Wrap(err, "error loading tls config")
		}
		conn = tls.Client(conn, cfg)
	}
	return conn, nil
}

func loadTLS(opts *tlsOpts) (*tls.Config, error) {
	cfg := &tls.Config{
		ServerName: opts.serverName,
		RootCAs:    x509.NewCertPool(),
	}

	if opts.caCert != "" {
		ca, err := os.ReadFile(opts.caCert)
		if err != nil {
			return nil, errors.Wrap(err, "could not read ca certificate")
		}
		if ok := cfg.RootCAs.AppendCertsFromPEM(ca); !ok {
			return nil, errors.New("failed to append ca certs")
		}
	}

	if opts.cert != "" || opts.key != "" {
		cert, err := tls.LoadX509KeyPair(opts.cert, opts.key)
		if err != nil {
			return nil, errors.Wrap(err, "could not read certificate/key")
		}
		cfg.Certificates = append(cfg.Certificates, cert)
	}

	return cfg, nil
}

func (d *Driver) Features(ctx context.Context) map[driver.Feature]bool {
	return map[driver.Feature]bool{
		driver.OCIExporter:    true,
		driver.DockerExporter: true,
		driver.CacheExport:    true,
		driver.MultiPlatform:  true,
		driver.DirectPush:     true,
		driver.DefaultLoad:    d.defaultLoad,
	}
}

func (d *Driver) HostGatewayIP(ctx context.Context) (net.IP, error) {
	return nil, errors.New("host-gateway is not supported by the remote driver")
}

func (d *Driver) Factory() driver.Factory {
	return d.factory
}

func (d *Driver) IsMobyDriver() bool {
	return false
}

func (d *Driver) Config() driver.InitConfig {
	return d.InitConfig
}
