//go:build exclude_frontend

package frontend

import "github.com/labstack/echo/v5"

func RegisterFrontend(e *echo.Echo, basePath string) error {
	_, _ = e, basePath
	return ErrFrontendNotIncluded
}
