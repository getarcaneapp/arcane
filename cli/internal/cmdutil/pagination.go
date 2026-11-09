package cmdutil

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"kit.getarcane.app"

	"getarcane.dev/cli/v3/internal/config"
	"getarcane.dev/cli/v3/internal/runtime"
)

type (
	ListParams struct {
		Resource        string
		Limit           int
		FallbackDefault int
		Start           int
		All             bool
	}
)

const (
	ShowAllLimit   = -1
	AllFlagUsage   = "Return every item, ignoring pagination"
	StartFlagUsage = "Offset for pagination (rounded down to a multiple of --limit on database-backed resources)"
)

func AppendQuery(path string, extra url.Values) string {
	parsed, err := url.Parse(path)
	if err != nil {
		return path
	}
	query := parsed.Query()
	for key, values := range extra {
		for _, value := range values {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func ApplyPaginationParams(cmd *cobra.Command, path string, p ListParams) (string, error) {
	parsed, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("failed to parse path: %w", err)
	}

	query := parsed.Query()

	if p.All {
		if cmd != nil && (cmd.Flags().Changed("limit") || cmd.Flags().Changed("start")) {
			return "", errors.New("--all cannot be combined with --limit or --start")
		}
		query.Set("limit", strconv.Itoa(ShowAllLimit))
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}

	if effectiveLimit := EffectiveLimit(cmd, p.Resource, "limit", p.Limit, p.FallbackDefault); effectiveLimit != 0 {
		query.Set("limit", strconv.Itoa(effectiveLimit))
	}
	if cmd != nil {
		if flag := cmd.Flags().Lookup("start"); flag != nil && flag.Changed {
			query.Set("start", strconv.Itoa(p.Start))
		}
	}

	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func EffectiveLimit(cmd *cobra.Command, resource, flagName string, flagValue, fallbackDefault int) int {
	if cmd != nil {
		if flag := cmd.Flags().Lookup(flagName); flag != nil && flag.Changed {
			return kit.Ternary(flagValue > 0 || flagValue == ShowAllLimit, flagValue, 0)
		}
	}

	var ctx context.Context
	if cmd != nil {
		ctx = cmd.Context()
	}
	if app, ok := runtime.From(ctx); ok {
		if cfg := app.Config(); cfg != nil {
			if v := cfg.LimitFor(resource); v != 0 {
				return v
			}
		}
	} else if cfg, err := config.Load(cmp.Or(ctx, context.Background())); err == nil && cfg != nil {
		if v := cfg.LimitFor(resource); v != 0 {
			return v
		}
	}

	if fallbackDefault > 0 {
		return fallbackDefault
	}
	return kit.Ternary(flagValue > 0, flagValue, 0)
}
