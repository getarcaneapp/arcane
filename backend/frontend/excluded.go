//go:build exclude_frontend

package frontend

import "github.com/labstack/echo/v4"

func RegisterFrontend(e *echo.Echo) error {
	_ = e
	return ErrFrontendNotIncluded
}
