// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package manager

import (
	"fmt"
)

// pluginError is set as Plugin.Err by NewPlugin if the plugin
// candidate fails one of the candidate tests. This exists primarily
// to implement encoding.TextMarshaller such that rendering a plugin as JSON
// (e.g. for `docker info -f '{{json .CLIPlugins}}'`) renders the Err
// field as a useful string and not just `{}`. See
// https://github.com/golang/go/issues/10748 for some discussion
// around why the builtin error type doesn't implement this.
type pluginError struct {
	cause error
}

// Error satisfies the core error interface for pluginError.
func (e *pluginError) Error() string {
	return e.cause.Error()
}

// Unwrap provides compatibility for Go 1.13 error chains.
func (e *pluginError) Unwrap() error {
	return e.cause
}

// MarshalText marshalls the pluginError into a textual form.
func (e *pluginError) MarshalText() (text []byte, err error) {
	return []byte(e.cause.Error()), nil
}

// wrapAsPluginError wraps an error in a pluginError with an
// additional message.
func wrapAsPluginError(err error, msg string) error {
	return &pluginError{cause: fmt.Errorf("%s: %w", msg, err)}
}

// newPluginError creates a new pluginError, analogous to
// errors.Errorf.
func newPluginError(msg string, args ...any) error {
	return &pluginError{cause: fmt.Errorf(msg, args...)}
}
