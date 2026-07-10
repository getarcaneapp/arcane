package handlers

import (
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestUpdateMyProfileTimeFormatValidation(t *testing.T) {
	registry := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	schema := registry.Schema(reflect.TypeOf(UpdateMyProfileInput{}.Body), true, "UpdateMyProfileBody")

	for _, test := range []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "auto", value: "auto"},
		{name: "12 hour", value: "12h"},
		{name: "24 hour", value: "24h"},
		{name: "invalid", value: "military", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := &huma.ValidateResult{}
			huma.Validate(
				registry,
				schema,
				huma.NewPathBuffer(nil, 0),
				huma.ModeWriteToServer,
				map[string]any{"timeFormat": test.value},
				result,
			)

			if test.wantError {
				require.NotEmpty(t, result.Errors)
				return
			}
			require.Empty(t, result.Errors)
		})
	}
}
