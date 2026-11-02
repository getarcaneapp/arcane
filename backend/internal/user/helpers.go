package user

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
	"uuid"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/types/v3"
)

const (
	maxCustomThemes          = 20
	dashboardColumns         = 12
	maxDashboardWidgets      = 50
	maxDashboardWidgetTitle  = 80
	maxDashboardWidgetText   = 256
	maxDashboardWidgetLimit  = 50
	maxDashboardEnvironments = 50
	maxDashboardFilters      = 20
	maxDashboardFilterKey    = 64
)

var (
	dashboardWidgetSizes = map[string]struct {
		minW, minH, maxH int
		multi            bool
	}{
		"healthy":          {minW: 2, minH: 2, maxH: 4},
		"attentionCount":   {minW: 2, minH: 2, maxH: 4},
		"updates":          {minW: 2, minH: 2, maxH: 4},
		"risk":             {minW: 2, minH: 2, maxH: 4},
		"reclaimableTotal": {minW: 2, minH: 2, maxH: 4},
		"environments":     {minW: 6, minH: 3, maxH: 12},
		"attention":        {minW: 4, minH: 4, maxH: 12},
		"activity":         {minW: 4, minH: 4, maxH: 12},
		"jobs":             {minW: 3, minH: 3, maxH: 12},
		"reclaimable":      {minW: 3, minH: 3, maxH: 12},
		"memory":           {minW: 3, minH: 3, maxH: 12},
		"resourceQuery":    {minW: 2, minH: 2, maxH: 12, multi: true},
		"resource":         {minW: 3, minH: 2, maxH: 6, multi: true},
		"containerStats":   {minW: 4, minH: 5, maxH: 12, multi: true},
		"containerLogs":    {minW: 4, minH: 4, maxH: 12, multi: true},
		"environment":      {minW: 3, minH: 3, maxH: 6, multi: true},
	}
	dashboardResourceKinds = []string{"containers", "projects", "images", "volumes", "networks"}
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

func normalizeDashboardLayout(layout []types.DashboardWidgetLayout) []types.DashboardWidgetLayout {
	widgets := make([]types.DashboardWidgetLayout, 0, min(len(layout), maxDashboardWidgets))
	for _, widget := range layout {
		if len(widgets) == maxDashboardWidgets {
			break
		}
		widgetType := cmp.Or(widget.Type, widget.ID)
		sizes, known := dashboardWidgetSizes[widgetType]
		id := kit.Ternary(sizes.multi, cmp.Or(widget.ID, uuid.New().String()), widget.ID)
		duplicate := slices.ContainsFunc(widgets, func(existing types.DashboardWidgetLayout) bool {
			return existing.ID == id
		})
		if !known || id == "" || duplicate || (!sizes.multi && id != widgetType) {
			continue
		}
		width := min(max(widget.W, sizes.minW), dashboardColumns)
		widgets = append(widgets, types.DashboardWidgetLayout{
			ID:     id,
			Type:   widgetType,
			Title:  truncateRunes(strings.TrimSpace(widget.Title), maxDashboardWidgetTitle),
			X:      min(max(widget.X, 0), dashboardColumns-width),
			Y:      max(widget.Y, 0),
			W:      width,
			H:      min(max(widget.H, sizes.minH), sizes.maxH),
			Hidden: widget.Hidden,
			Config: kit.Ternary(sizes.multi, sanitizeDashboardWidgetConfig(widget.Config), nil),
		})
	}
	return widgets
}

func sanitizeDashboardWidgetConfig(config *types.DashboardWidgetConfig) *types.DashboardWidgetConfig {
	if config == nil {
		return nil
	}
	environmentIDs := kit.Unique(kit.TrimNonEmpty(config.EnvironmentIDs))
	filters := make(map[string]string, min(len(config.Filters), maxDashboardFilters))
	for _, key := range slices.Sorted(maps.Keys(config.Filters)) {
		name := truncateRunes(strings.TrimSpace(key), maxDashboardFilterKey)
		if name == "" {
			continue
		}
		if len(filters) == maxDashboardFilters {
			break
		}
		filters[name] = truncateRunes(config.Filters[key], maxDashboardWidgetText)
	}
	return &types.DashboardWidgetConfig{
		EnvironmentIDs: environmentIDs[:min(len(environmentIDs), maxDashboardEnvironments)],
		ResourceKind:   kit.Ternary(slices.Contains(dashboardResourceKinds, config.ResourceKind), config.ResourceKind, ""),
		ResourceID:     truncateRunes(strings.TrimSpace(config.ResourceID), maxDashboardWidgetText),
		Search:         truncateRunes(config.Search, maxDashboardWidgetText),
		Filters:        filters,
		Display:        kit.Ternary(config.Display == "list", "list", "count"),
		Limit:          min(max(cmp.Or(config.Limit, 5), 1), maxDashboardWidgetLimit),
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	return string(runes[:min(len(runes), limit)])
}
