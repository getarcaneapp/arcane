package middleware

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v5"
)

func EchoHandler(handler echo.HandlerFunc) func(context.Context, *struct{}) (*huma.StreamResponse, error) {
	return func(context.Context, *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{
			Body: func(ctx huma.Context) {
				c := humaecho.Unwrap(ctx)
				if err := handler(c); err != nil {
					c.Echo().HTTPErrorHandler(c, err)
				}
			},
		}, nil
	}
}

func EchoMiddleware(mw echo.MiddlewareFunc) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		c := humaecho.Unwrap(ctx)
		if err := mw(func(*echo.Context) error {
			next(ctx)
			return nil
		})(c); err != nil {
			c.Echo().HTTPErrorHandler(c, err)
		}
	}
}
