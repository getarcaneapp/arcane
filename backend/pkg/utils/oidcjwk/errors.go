package oidcjwk

import "regexp"

type (
	// Cache errors include endpoint and redirect URLs, which may contain credentials.
	initializationError struct {
		cause error
	}
)

var errorURLPattern = regexp.MustCompile(`https?://[^\s"<>]+`)

func (e *initializationError) Error() string {
	return "failed to initialize JWKS: " + errorURLPattern.ReplaceAllString(e.cause.Error(), "[redacted URL]")
}

func (e *initializationError) Unwrap() error {
	return e.cause
}
