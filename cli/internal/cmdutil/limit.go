package cmdutil

import (
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/cli/internal/config"
	runtimectx "github.com/getarcaneapp/arcane/cli/internal/runtime"
	clitypes "github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/spf13/cobra"
)

// EffectiveLimit resolves the final list limit with precedence:
// explicit flag > per-resource config > global config > fallback default.
func EffectiveLimit(cmd *cobra.Command, resource, flagName string, flagValue, fallbackDefault int) int {
	if cmd != nil {
		if flag := cmd.Flags().Lookup(flagName); flag != nil && flag.Changed {
			if flagValue > 0 {
				return flagValue
			}
			return 0
		}
	}

	resource = clitypes.NormalizePaginatedResource(resource)
	if app, ok := runtimectx.From(cmd.Context()); ok {
		if cfg := app.Config(); cfg != nil {
			if v := cfg.LimitFor(resource); v > 0 {
				return v
			}
		}
	} else if cfg, err := config.Load(); err == nil && cfg != nil {
		if v := cfg.LimitFor(resource); v > 0 {
			return v
		}
	}

	if fallbackDefault > 0 {
		return fallbackDefault
	}
	if flagValue > 0 {
		return flagValue
	}
	return 0
}

// ParseResourceLimit parses a "resource=limit" pair.
func ParseResourceLimit(pair string) (resource string, limit int, ok bool) {
	left, right, found := strings.Cut(pair, "=")
	if !found {
		return "", 0, false
	}
	resource = clitypes.NormalizePaginatedResource(left)
	if resource == "" {
		return "", 0, false
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil {
		return "", 0, false
	}
	return resource, parsed, true
}
