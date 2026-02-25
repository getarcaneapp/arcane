// Package registry is derived from Moby's registry implementation.
//
// Source: https://github.com/moby/moby/blob/v28.5.2/registry/auth.go
// License: Apache License 2.0
// SPDX-License-Identifier: Apache-2.0
//
// # Simplified Wrapper as Arcane only needs the Ping Function currently
//
// This local copy is vendored to avoid pulling conflicting module variants
// during dependency resolution in this monorepo.
package registry

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/docker/distribution/registry/client/auth/challenge"
)

type PingResponseError struct {
	Err error
}

func (e PingResponseError) Error() string {
	if e.Err == nil {
		return "registry ping response error"
	}

	return e.Err.Error()
}

func (e PingResponseError) Unwrap() error {
	return e.Err
}

// PingV2Registry attempts to ping a v2 registry and on success return a
// challenge manager for the supported authentication types.
// If a response is received but cannot be interpreted, a PingResponseError will be returned.
func PingV2Registry(ctx context.Context, endpoint *url.URL, transport http.RoundTripper) (challenge.Manager, error) {
	pingClient := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}
	endpointStr := strings.TrimRight(endpoint.String(), "/") + "/v2/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointStr, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := pingClient.Do(req) //nolint:gosec // intentional request to user-configured registry endpoint
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	challengeManager := challenge.NewSimpleManager()
	if err := challengeManager.AddResponse(resp); err != nil {
		return nil, PingResponseError{
			Err: err,
		}
	}

	return challengeManager, nil
}
