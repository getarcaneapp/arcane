// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package registry

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/docker/distribution/registry/api/errcode"
)

func translateV2AuthError(err error) error {
	var e *url.Error
	if errors.As(err, &e) {
		var e2 errcode.Error
		if errors.As(e, &e2) && errors.Is(e2.Code, errcode.ErrorCodeUnauthorized) {
			return unauthorizedErr{err}
		}
	}
	return err
}

func invalidParam(err error) error {
	return invalidParameterErr{err}
}

func invalidParamf(format string, args ...any) error {
	return invalidParameterErr{fmt.Errorf(format, args...)}
}

type unauthorizedErr struct{ error }

func (unauthorizedErr) Unauthorized() {}

func (e unauthorizedErr) Cause() error {
	return e.error
}

func (e unauthorizedErr) Unwrap() error {
	return e.error
}

type invalidParameterErr struct{ error }

func (invalidParameterErr) InvalidParameter() {}

func (e invalidParameterErr) Unwrap() error {
	return e.error
}
