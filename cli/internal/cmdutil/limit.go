package cmdutil

import (
	"github.com/getarcaneapp/arcane/cli/v2/internal/config"
	runtimectx "github.com/getarcaneapp/arcane/cli/v2/internal/runtime"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
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
	if cmd != nil {
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
