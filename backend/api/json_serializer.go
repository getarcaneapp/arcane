package api

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"
	"go.getarcane.app/kit/normalization"
)

type jsonV2Serializer struct{}

// Preserve the established numeric wire format for duration fields in third-party API types.
var jsonV2APIOptions = jsonv1.FormatDurationAsNano(true)

func (jsonV2Serializer) Serialize(c *echo.Context, value any, indent string) error {
	if indent != "" {
		return json.MarshalWrite(c.Response(), value, jsonV2APIOptions, jsontext.WithIndent(indent))
	}

	return json.MarshalWrite(c.Response(), value, jsonV2APIOptions)
}

func (jsonV2Serializer) Deserialize(c *echo.Context, value any) error {
	err := json.UnmarshalRead(c.Request().Body, value, jsonV2APIOptions)
	if err == nil {
		if err := normalization.Normalize(value); err != nil {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error()).Wrap(err)
		}
		return nil
	}

	if semanticErr, ok := errors.AsType[*json.SemanticError](err); ok {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			fmt.Sprintf(
				"Unmarshal type error: expected=%v, got=%v, field=%v, offset=%v",
				semanticErr.GoType,
				semanticErr.JSONKind,
				semanticErr.JSONPointer,
				semanticErr.ByteOffset,
			),
		).Wrap(err)
	}

	if syntacticErr, ok := errors.AsType[*jsontext.SyntacticError](err); ok {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			fmt.Sprintf("Syntax error: offset=%v, error=%v", syntacticErr.ByteOffset, syntacticErr.Error()),
		).Wrap(err)
	}

	return err
}
