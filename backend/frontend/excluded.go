//go:build exclude_frontend

package frontend

import "github.com/labstack/echo/v5"

//nolint:shimbad // build-tag stub: the real implementation lives in frontend.go
func RegisterFrontend(e *echo.Echo) error {
	_ = e
	return ErrFrontendNotIncluded
}
