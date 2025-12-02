package registry

import (
	"context"
	"net/url"

	"github.com/docker/go-connections/tlsconfig"
)

func (s *Service) Endpoints(ctx context.Context, hostname string) ([]APIEndpoint, error) {
	if hostname == DefaultNamespace || hostname == IndexHostname {
		return []APIEndpoint{{
			URL:       DefaultV2Registry,
			TLSConfig: tlsconfig.ServerDefault(),
		}}, nil
	}

	tlsConfig, err := newTLSConfig(ctx, hostname, s.config.isSecureIndex(hostname))
	if err != nil {
		return nil, err
	}

	endpoints := []APIEndpoint{{
		URL:       &url.URL{Scheme: "https", Host: hostname},
		TLSConfig: tlsConfig,
	}}

	if tlsConfig.InsecureSkipVerify {
		endpoints = append(endpoints, APIEndpoint{
			URL: &url.URL{Scheme: "http", Host: hostname},
			// used to check if supposed to be secure via InsecureSkipVerify
			TLSConfig: tlsConfig,
		})
	}

	return endpoints, nil
}
