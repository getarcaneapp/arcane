package frontend

import "errors"

var ErrFrontendNotIncluded = errors.New("frontend is not included")

// ErrIndexNotRecognized means the built index.html could not be adapted to the base path, so
// the UI cannot be served under it.
var ErrIndexNotRecognized = errors.New("index.html: SvelteKit base not found; cannot serve under a base path")
