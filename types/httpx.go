package types

import (
	"time"
)

type (
	// HTTPClientOptions configures timeouts for an outbound HTTP client.
	// Durations are used as provided; a zero Timeout disables the request timeout.
	HTTPClientOptions struct {
		Timeout               time.Duration
		TLSHandshakeTimeout   time.Duration
		DialTimeout           time.Duration
		ResponseHeaderTimeout time.Duration
	}
)
