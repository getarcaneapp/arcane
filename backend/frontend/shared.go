package frontend

import "errors"

var (
	ErrFrontendNotIncluded = errors.New("frontend is not included")

	ErrIndexNotRecognized = errors.New("index.html: SvelteKit base not found; cannot serve under a base path")
)
