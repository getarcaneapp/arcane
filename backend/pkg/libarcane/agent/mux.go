package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/hashicorp/yamux"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	// tunnelCapabilityMux is offered by agents that can run the multiplexed data plane.
	tunnelCapabilityMux = "mux-v1"
	// tunnelCapabilityMuxGrant switches both peers to mux; only managers that serve mux send it, since older ones echo offers.
	tunnelCapabilityMuxGrant = "mux-grant-v1"

	// MuxAgentBaseURL is the synthetic origin for requests a manager sends through a multiplexed tunnel.
	MuxAgentBaseURL   = "http://agent.tunnel.arcane.internal"
	muxManagerBaseURL = "http://manager.tunnel.arcane.internal"
	muxEventsPath     = "/api/tunnel/events"

	muxStreamWindowSize = 4 << 20
	muxMaxFrameSize     = 1 << 20
	muxMaxPendingWrite  = 1 << 20
	muxWriteTimeout     = 30 * time.Second
	// muxDialTimeout bounds a stream open on its own, because http.Transport detaches dials from request cancellation.
	muxDialTimeout = 30 * time.Second
	// ProxyResponseHeaderTimeout is generous because proxied operations such as deploys can run for minutes before responding.
	ProxyResponseHeaderTimeout = 30 * time.Minute
)

var (
	// errTunnelMuxActive rejects register messages once a connection carries a multiplexed session.
	errTunnelMuxActive    = errors.New("agent tunnel connection carries a multiplexed session")
	errAgentMuxRequired   = errors.New("agent does not support the multiplexed data plane; upgrade the agent")
	errManagerMuxRequired = errors.New("manager did not grant the multiplexed data plane; upgrade the manager")
)

// newMuxConfig returns the yamux settings both peers use. Only the agent enables yamux keepalive; the manager
// pings explicitly from serveMuxTunnel to record RTT and heartbeats.
func newMuxConfig(keepAlive bool) *yamux.Config {
	return &yamux.Config{
		AcceptBacklog:          256,
		EnableKeepAlive:        keepAlive,
		KeepAliveInterval:      DefaultHeartbeatInterval,
		ConnectionWriteTimeout: muxWriteTimeout,
		MaxStreamWindowSize:    muxStreamWindowSize,
		StreamOpenTimeout:      75 * time.Second,
		StreamCloseTimeout:     5 * time.Minute,
		Logger:                 slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
	}
}

// newMuxHTTPClient returns a traced client whose connections are streams opened by dial.
func newMuxHTTPClient(dial func(ctx context.Context) (net.Conn, error)) *http.Client {
	return &http.Client{Transport: otelhttp.NewTransport(&http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialCtx, cancel := context.WithTimeout(ctx, muxDialTimeout)
			defer cancel()
			return dial(dialCtx)
		},
		// Stays below the agent server's IdleTimeout so a pooled stream is never reused as the server closes it.
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   32,
		ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: ProxyResponseHeaderTimeout,
		ReadBufferSize:        64 << 10,
		WriteBufferSize:       64 << 10,
	})}
}

// openMuxStream opens a stream but returns as soon as ctx ends. yamux's Open cannot be cancelled while it
// waits for an in-flight SYN slot, so a stream that opens after the caller left is closed; no other stream is touched.
func openMuxStream(ctx context.Context, session *yamux.Session) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type openResult struct {
		conn net.Conn
		err  error
	}
	opened := make(chan openResult, 1)
	go func() {
		conn, err := session.Open()
		opened <- openResult{conn: conn, err: err}
	}()
	select {
	case result := <-opened:
		if result.err != nil {
			return nil, fmt.Errorf("open agent tunnel stream: %w: %w", ErrTunnelConnectionClosed, result.err)
		}
		return result.conn, nil
	case <-ctx.Done():
		go func() {
			if result := <-opened; result.conn != nil {
				_ = result.conn.Close()
			}
		}()
		// A session closing races the dial context; report it as a closed tunnel either way.
		if session.IsClosed() {
			return nil, fmt.Errorf("open agent tunnel stream: %w", ErrTunnelConnectionClosed)
		}
		return nil, ctx.Err()
	}
}

// newMuxConn starts the sender that coalesces writes, so a yamux header and body share one transport message.
// Batches are never reused, since gRPC forbids modifying a message after Send.
func newMuxConn(send func([]byte) error, recv func() ([]byte, error), closeTransport func() error) *muxConn {
	c := &muxConn{recv: recv, closeTransport: closeTransport}
	c.cond.L = &c.mu
	go func() {
		for {
			c.mu.Lock()
			for c.err == nil && len(c.pending) == 0 {
				c.cond.Wait()
			}
			batch, err := c.pending, c.err
			c.pending = nil
			c.cond.Broadcast()
			c.mu.Unlock()
			if err != nil {
				return
			}
			for frame := range slices.Chunk(batch, muxMaxFrameSize) {
				if sendErr := send(frame); sendErr != nil {
					c.fail(sendErr)
					return
				}
			}
		}
	}()
	return c
}

func (c *muxConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.err == nil && len(c.pending) >= muxMaxPendingWrite {
		c.cond.Wait()
	}
	if c.err != nil {
		return 0, c.err
	}
	c.pending = append(c.pending, p...)
	c.cond.Broadcast()
	return len(p), nil
}

// Read is called only by the yamux receive loop.
func (c *muxConn) Read(p []byte) (int, error) {
	for len(c.unread) == 0 {
		frame, err := c.recv()
		if err != nil {
			c.fail(err)
			return 0, err
		}
		c.unread = frame
	}
	n := copy(p, c.unread)
	c.unread = c.unread[n:]
	return n, nil
}

func (c *muxConn) Close() error {
	c.fail(net.ErrClosed)
	var err error
	c.closeOnce.Do(func() { err = c.closeTransport() })
	return err
}

// fail records the first error and wakes blocked writers and the sender.
func (c *muxConn) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.cond.Broadcast()
	c.mu.Unlock()
}
