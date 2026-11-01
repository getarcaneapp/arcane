package httpx

import (
	"cmp"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"kit.getarcane.app"
)

type (
	TransportConfig struct {
		TLSClientConfig       *tls.Config
		DialContext           func(ctx context.Context, network, address string) (net.Conn, error)
		IdleConnTimeout       time.Duration
		ResponseHeaderTimeout time.Duration
		MaxIdleConnsPerHost   int
		ReadBufferSize        int
		WriteBufferSize       int
		DisableProxy          bool
	}

	ClientConfig struct {
		TransportConfig
		Timeout    time.Duration
		Instrument bool
	}
)

var defaultTransport = http.DefaultTransport.(*http.Transport).Clone()

func NewTransport(cfg TransportConfig) *http.Transport {
	return &http.Transport{
		Proxy:                 kit.Ternary(cfg.DisableProxy, nil, defaultTransport.Proxy),
		DialContext:           kit.Ternary(cfg.DialContext != nil, cfg.DialContext, defaultTransport.DialContext),
		TLSClientConfig:       cfg.TLSClientConfig,
		TLSHandshakeTimeout:   defaultTransport.TLSHandshakeTimeout,
		MaxIdleConns:          defaultTransport.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       cmp.Or(cfg.IdleConnTimeout, defaultTransport.IdleConnTimeout),
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		ExpectContinueTimeout: defaultTransport.ExpectContinueTimeout,
		WriteBufferSize:       cfg.WriteBufferSize,
		ReadBufferSize:        cfg.ReadBufferSize,
		ForceAttemptHTTP2:     defaultTransport.ForceAttemptHTTP2,
	}
}

func NewHTTPClient(cfg ClientConfig) *http.Client {
	var transport http.RoundTripper = NewTransport(cfg.TransportConfig)
	if cfg.Instrument {
		transport = otelhttp.NewTransport(transport)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   max(cfg.Timeout, 0),
	}
}
