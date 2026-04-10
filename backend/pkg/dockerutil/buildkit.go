package docker

import (
	"context"
	"net"

	bkclient "github.com/moby/buildkit/client"
	"github.com/moby/moby/client"
)

/*
This code is adapted from:
https://github.com/moby/moby/blob/v28.5.2/client/buildkit/buildkit.go

Original source: moby/moby
License: Apache-2.0 (https://github.com/moby/moby/blob/v28.5.2/LICENSE)

Changes for Arcane: package name and client import path adjusted.
*/

// ClientOpts returns a list of buildkit client options which allows the
// caller to create a buildkit client which will connect to the buildkit
// API provided by the daemon. These options can be passed to [bkclient.New].
//
// Example:
//
//	bkclient.New(ctx, "", ClientOpts(c)...)
func ClientOpts(c client.HijackDialer) []bkclient.ClientOpt {
	return []bkclient.ClientOpt{
		bkclient.WithSessionDialer(func(ctx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
			return c.DialHijack(ctx, "/session", proto, meta)
		}),
		bkclient.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return c.DialHijack(ctx, "/grpc", "h2c", nil)
		}),
	}
}
