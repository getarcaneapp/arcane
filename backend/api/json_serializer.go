package api

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"net/http"

	"emperror.dev/errors"

	"github.com/labstack/echo/v4"
)

type jsonV2Serializer struct{}

// Preserve the established numeric wire format for duration fields in third-party API types.
var jsonV2APIOptions = jsonv1.FormatDurationAsNano(true)

func (jsonV2Serializer) Serialize(c echo.Context, value any, indent string) error {
	if indent != "" {
		return json.MarshalWrite(c.Response(), value, jsonV2APIOptions, jsontext.WithIndent(indent))
	}

	return json.MarshalWrite(c.Response(), value, jsonV2APIOptions)
}

func (jsonV2Serializer) Deserialize(c echo.Context, value any) error {
	err := json.UnmarshalRead(c.Request().Body, value, jsonV2APIOptions)
	if err == nil {
		return nil
	}

	var semanticErr *json.SemanticError
	if errors.As(err, &semanticErr) {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			fmt.Sprintf(
				"Unmarshal type error: expected=%v, got=%v, field=%v, offset=%v",
				semanticErr.GoType,
				semanticErr.JSONKind,
				semanticErr.JSONPointer,
				semanticErr.ByteOffset,
			),
		).SetInternal(err)
	}

	var syntacticErr *jsontext.SyntacticError
	if errors.As(err, &syntacticErr) {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			fmt.Sprintf("Syntax error: offset=%v, error=%v", syntacticErr.ByteOffset, syntacticErr.Error()),
		).SetInternal(err)
	}

	return err
}
