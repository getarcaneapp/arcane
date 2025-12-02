package registry

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	"github.com/docker/docker/api/types/registry"
)

// Service is a registry service. It tracks configuration data such as a list
// of mirrors.
type Service struct {
	config *serviceConfig
}

// NewService returns a new instance of [Service] ready to be installed into
// an engine.
func NewService(options ServiceOptions) (*Service, error) {
	config, err := newServiceConfig(options.InsecureRegistries)
	if err != nil {
		return nil, err
	}
	return &Service{config: config}, nil
}

// Auth contacts the public registry with the provided credentials,
// and returns OK if authentication was successful.
// It can be used to verify the validity of a client's credentials.
func (s *Service) Auth(ctx context.Context, authConfig *registry.AuthConfig, userAgent string) (token string, _ error) {
	registryHostName := IndexHostname

	if authConfig.ServerAddress != "" {
		serverAddress := authConfig.ServerAddress
		if !strings.HasPrefix(serverAddress, "https://") && !strings.HasPrefix(serverAddress, "http://") {
			serverAddress = "https://" + serverAddress
		}
		u, err := url.Parse(serverAddress)
		if err != nil {
			return "", invalidParam(fmt.Errorf("unable to parse server address: %w", err))
		}
		registryHostName = u.Host
	}

	// Lookup endpoints for authentication.
	endpoints, err := s.Endpoints(ctx, registryHostName)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", invalidParam(err)
	}

	var lastErr error
	for _, endpoint := range endpoints {
		authToken, err := loginV2(ctx, authConfig, endpoint, userAgent)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errdefs.IsUnauthorized(err) {
				// Failed to authenticate; don't continue with (non-TLS) endpoints.
				return "", err
			}
			// Try next endpoint
			log.G(ctx).WithFields(log.Fields{
				"error":    err,
				"endpoint": endpoint,
			}).Infof("Error logging in to endpoint, trying next endpoint")
			lastErr = err
			continue
		}

		return authToken, nil
	}

	return "", lastErr
}

// APIEndpoint represents a remote API endpoint
type APIEndpoint struct {
	URL       *url.URL
	TLSConfig *tls.Config
}
