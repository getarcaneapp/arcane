package driver

import (
	"context"
	"io"
	"net"
	"strings"

	"github.com/docker/buildx/store"
	"github.com/docker/buildx/util/progress"
	clitypes "github.com/docker/cli/cli/config/types"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

type ErrNotRunning struct{}

func (ErrNotRunning) Error() string {
	return "driver not running"
}

type ErrNotConnecting struct{}

func (ErrNotConnecting) Error() string {
	return "driver not connecting"
}

type Status int

const (
	Inactive Status = iota
	Starting
	Running
	Stopping
	Stopped
)

func (s Status) String() string {
	switch s {
	case Inactive:
		return "inactive"
	case Starting:
		return "starting"
	case Running:
		return "running"
	case Stopping:
		return "stopping"
	case Stopped:
		return "stopped"
	}
	return "unknown"
}

type Info struct {
	Status Status
	// DynamicNodes must be empty if the actual nodes are statically listed in the store
	DynamicNodes []store.Node
}

type Auth interface {
	GetAuthConfig(registryHostname string) (clitypes.AuthConfig, error)
}

type Driver interface {
	Factory() Factory
	Bootstrap(context.Context, progress.Logger) error
	Info(context.Context) (*Info, error)
	Version(context.Context) (string, error)
	Stop(ctx context.Context, force bool) error
	Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error
	Dial(ctx context.Context) (net.Conn, error)
	Client(ctx context.Context, opts ...client.ClientOpt) (*client.Client, error)
	Features(ctx context.Context) map[Feature]bool
	HostGatewayIP(ctx context.Context) (net.IP, error)
	IsMobyDriver() bool
	Config() InitConfig
}

const builderNamePrefix = "buildx_buildkit_"

func BuilderName(name string) string {
	return builderNamePrefix + name
}

func ParseBuilderName(name string) (string, error) {
	if !strings.HasPrefix(name, builderNamePrefix) {
		return "", errors.Errorf("invalid builder name %q, must have %q prefix", name, builderNamePrefix)
	}
	return strings.TrimPrefix(name, builderNamePrefix), nil
}

func Boot(ctx, clientContext context.Context, d *DriverHandle, pw progress.Writer) (*client.Client, error) {
	try := 0
	logger := discardLogger
	if pw != nil {
		logger = pw.Write
	}

	for {
		info, err := d.Info(ctx)
		if err != nil {
			return nil, err
		}
		try++
		if info.Status != Running {
			if try > 2 {
				return nil, errors.Errorf("failed to bootstrap %T driver in attempts", d)
			}
			if err := d.Bootstrap(ctx, logger); err != nil {
				return nil, err
			}
		}

		c, err := d.Client(clientContext)
		if err != nil {
			if errors.Is(err, ErrNotRunning{}) && try <= 2 {
				continue
			}
			return nil, err
		}
		return c, nil
	}
}

func discardLogger(*client.SolveStatus) {}

func historyAPISupported(ctx context.Context, c *client.Client) bool {
	cl, err := c.ControlClient().ListenBuildHistory(ctx, &controlapi.BuildHistoryRequest{
		ActiveOnly: true,
		Ref:        "buildx-test-history-api-feature", // dummy ref to check if the server supports the API
		EarlyExit:  true,
	})
	if err != nil {
		return false
	}
	for {
		_, err := cl.Recv()
		if errors.Is(err, io.EOF) {
			return true
		} else if err != nil {
			return false
		}
	}
}
