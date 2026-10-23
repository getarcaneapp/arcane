package types

import (
	"time"
)

// HTTPClientOptions configures timeouts for an outbound HTTP client.
// Durations are used as provided; a zero Timeout disables the request timeout.
type HTTPClientOptions struct {
	Timeout               time.Duration
	TLSHandshakeTimeout   time.Duration
	DialTimeout           time.Duration
	ResponseHeaderTimeout time.Duration
}
