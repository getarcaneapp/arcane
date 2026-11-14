package middleware

import (
	"net/http"
	"slices"
	"strings"

	"github.com/labstack/echo/v5"

	"getarcane.dev/arcane/v3/internal/common"
)

func RejectDotSegmentParams(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if slices.ContainsFunc(c.PathValues(), hasDotSegment) {
			return c.JSON(http.StatusBadRequest, common.APIError{
				Code:    common.APIErrorCodeBadRequest,
				Message: "Path parameters must not contain '.' or '..' segments",
			})
		}
		return next(c)
	}
}

func hasDotSegment(pv echo.PathValue) bool {
	for segment := range strings.SplitSeq(pv.Value, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}
