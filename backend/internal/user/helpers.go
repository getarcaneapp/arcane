package user

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

const (
	maxCustomThemes = 20
)

// CurrentUserFromContext retrieves the authenticated persistence model.
func CurrentUserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(types.CurrentUserContextKey{}).(*User)
	return u, ok
}

func validateCustomThemes(themes []types.CustomTheme) error {
	if len(themes) > maxCustomThemes {
		return common.Classify(common.ErrValidation, &types.FieldError{
			Field: "customThemes",
			Err:   fmt.Errorf("at most %d custom themes are allowed", maxCustomThemes),
		})
	}
	for i, theme := range themes {
		name := strings.TrimSpace(theme.Name)
		switch {
		case !strings.HasPrefix(theme.ID, "custom-") || len(theme.ID) > 48 || strings.Trim(theme.ID, "abcdefghijklmnopqrstuvwxyz0123456789-") != "":
			return common.Classify(common.ErrValidation, &types.FieldError{
				Field: "customThemes",
				Err:   fmt.Errorf("custom theme %q: id must start with custom- and use at most 48 lowercase letters, digits, or hyphens", theme.ID),
			})
		case slices.ContainsFunc(themes[:i], func(other types.CustomTheme) bool {
			return other.ID == theme.ID
		}):
			return common.Classify(common.ErrValidation, &types.FieldError{
				Field: "customThemes",
				Err:   fmt.Errorf("custom theme %q: id is used more than once", theme.ID),
			})
		case name == "" || utf8.RuneCountInString(name) > 48:
			return common.Classify(common.ErrValidation, &types.FieldError{
				Field: "customThemes",
				Err:   fmt.Errorf("custom theme %q: name must be 1 to 48 characters", theme.ID),
			})
		}
		for _, mode := range []struct {
			name    string
			palette types.ThemePalette
		}{
			{name: "light", palette: theme.Light},
			{name: "dark", palette: theme.Dark},
		} {
			values := reflect.ValueOf(mode.palette)
			for j := range values.NumField() {
				if !types.SafeThemeColor.MatchString(values.Field(j).String()) {
					return common.Classify(common.ErrValidation, &types.FieldError{
						Field: "customThemes",
						Err:   fmt.Errorf("custom theme %q: %s token %q is not a valid color", theme.ID, mode.name, values.Type().Field(j).Tag.Get("json")),
					})
				}
			}
		}
	}
	return nil
}
