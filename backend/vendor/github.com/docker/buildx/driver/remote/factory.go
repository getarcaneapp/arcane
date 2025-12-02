package remote

import (
	"context"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/buildx/driver"
	util "github.com/docker/buildx/driver/remote/util"
	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"

	// import connhelpers for special url schemes
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
	_ "github.com/moby/buildkit/client/connhelper/kubepod"
	_ "github.com/moby/buildkit/client/connhelper/ssh"
)

const prioritySupported = 20
const priorityUnsupported = 90

func init() {
	driver.Register(&factory{})
}

type factory struct {
}

func (*factory) Name() string {
	return "remote"
}

func (*factory) Usage() string {
	return "remote"
}

func (*factory) Priority(ctx context.Context, endpoint string, api dockerclient.APIClient, dialMeta map[string][]string) int {
	if util.IsValidEndpoint(endpoint) != nil {
		return priorityUnsupported
	}
	return prioritySupported
}

func (f *factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if len(cfg.Files) > 0 {
		return nil, errors.Errorf("setting config file is not supported for remote driver")
	}
	if len(cfg.BuildkitdFlags) > 0 {
		return nil, errors.Errorf("setting buildkit flags is not supported for remote driver")
	}

	d := &Driver{
		factory:    f,
		InitConfig: cfg,
	}

	tls := &tlsOpts{}
	tlsEnabled := false
	for k, v := range cfg.DriverOpts {
		switch k {
		case "servername":
			tls.serverName = v
			tlsEnabled = true
		case "cacert":
			if !filepath.IsAbs(v) {
				return nil, errors.Errorf("non-absolute path '%s' provided for %s", v, k)
			}
			tls.caCert = v
			tlsEnabled = true
		case "cert":
			if !filepath.IsAbs(v) {
				return nil, errors.Errorf("non-absolute path '%s' provided for %s", v, k)
			}
			tls.cert = v
			tlsEnabled = true
		case "key":
			if !filepath.IsAbs(v) {
				return nil, errors.Errorf("non-absolute path '%s' provided for %s", v, k)
			}
			tls.key = v
			tlsEnabled = true
		case "default-load":
			parsed, err := strconv.ParseBool(v)
			if err != nil {
				return nil, err
			}
			d.defaultLoad = parsed
		default:
			return nil, errors.Errorf("invalid driver option %s for remote driver", k)
		}
	}

	if tlsEnabled {
		if tls.serverName == "" {
			// guess servername as hostname of target address
			uri, err := url.Parse(cfg.EndpointAddr)
			if err != nil {
				return nil, err
			}
			tls.serverName = uri.Hostname()
		}
		missing := []string{}
		if tls.caCert == "" {
			missing = append(missing, "cacert")
		}
		if tls.cert != "" && tls.key == "" {
			missing = append(missing, "key")
		}
		if tls.key != "" && tls.cert == "" {
			missing = append(missing, "cert")
		}
		if len(missing) > 0 {
			return nil, errors.Errorf("tls enabled, but missing keys %s", strings.Join(missing, ", "))
		}
		d.tlsOpts = tls
	}

	return d, nil
}

func (f *factory) AllowsInstances() bool {
	return true
}
