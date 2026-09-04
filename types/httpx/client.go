package httpx

import "time"

// ClientOptions configures timeouts for an outbound HTTP client.
// Durations are used as provided; a zero Timeout disables the request timeout.
type ClientOptions struct {
	Timeout             time.Duration
	TLSHandshakeTimeout time.Duration
}
