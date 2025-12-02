package imagetools

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/containerd/log"
	"github.com/distribution/reference"
	"github.com/docker/buildx/util/resolver"
	clitypes "github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/tracing"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
)

type Auth interface {
	GetAuthConfig(registryHostname string) (clitypes.AuthConfig, error)
}

type Opt struct {
	Auth           Auth
	RegistryConfig map[string]resolver.RegistryConfig
}

type Resolver struct {
	auth   docker.Authorizer
	hosts  docker.RegistryHosts
	buffer contentutil.Buffer
}

func New(opt Opt) *Resolver {
	ac := newAuthConfig(opt.Auth)
	dockerAuth := docker.NewDockerAuthorizer(docker.WithAuthCreds(ac.credentials), docker.WithAuthClient(http.DefaultClient))
	auth := &withBearerAuthorizer{
		Authorizer: dockerAuth,
		AuthConfig: ac,
	}
	return &Resolver{
		auth:   auth,
		hosts:  resolver.NewRegistryConfig(opt.RegistryConfig),
		buffer: contentutil.NewBuffer(),
	}
}

func (r *Resolver) resolver() remotes.Resolver {
	return docker.NewResolver(docker.ResolverOptions{
		Hosts: func(domain string) ([]docker.RegistryHost, error) {
			res, err := r.hosts(domain)
			if err != nil {
				return nil, err
			}
			for i := range res {
				res[i].Authorizer = r.auth
				res[i].Client = tracing.DefaultClient
			}
			return res, nil
		},
	})
}

func (r *Resolver) Resolve(ctx context.Context, in string) (string, ocispecs.Descriptor, error) {
	// discard containerd logger to avoid printing unnecessary info during image reference resolution.
	// https://github.com/containerd/containerd/blob/1a88cf5242445657258e0c744def5017d7cfb492/remotes/docker/resolver.go#L288
	logger := logrus.New()
	logger.Out = io.Discard
	ctx = log.WithLogger(ctx, logrus.NewEntry(logger))

	ref, err := parseRef(in)
	if err != nil {
		return "", ocispecs.Descriptor{}, err
	}

	in, desc, err := r.resolver().Resolve(ctx, ref.String())
	if err != nil {
		return "", ocispecs.Descriptor{}, err
	}

	return in, desc, nil
}

func (r *Resolver) Get(ctx context.Context, in string) ([]byte, ocispecs.Descriptor, error) {
	in, desc, err := r.Resolve(ctx, in)
	if err != nil {
		return nil, ocispecs.Descriptor{}, err
	}

	dt, err := r.GetDescriptor(ctx, in, desc)
	if err != nil {
		return nil, ocispecs.Descriptor{}, err
	}
	return dt, desc, nil
}

func (r *Resolver) GetDescriptor(ctx context.Context, in string, desc ocispecs.Descriptor) ([]byte, error) {
	fetcher, err := r.resolver().Fetcher(ctx, in)
	if err != nil {
		return nil, err
	}

	rc, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, rc)
	rc.Close()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func parseRef(s string) (reference.Named, error) {
	ref, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return nil, err
	}
	ref = reference.TagNameOnly(ref)
	return ref, nil
}
