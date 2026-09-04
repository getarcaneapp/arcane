package httpx

import (
	"net/http"
	"time"

	httpxtypes "github.com/getarcaneapp/arcane/types/v2/httpx"
)

// NewHTTPClient builds a client with its own transport and the supplied timeouts.
func NewHTTPClient(options httpxtypes.ClientOptions) *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   options.TLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   options.Timeout,
	}
}
