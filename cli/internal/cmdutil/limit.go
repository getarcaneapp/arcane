package cmdutil

import (
	"context"

	"github.com/getarcaneapp/arcane/cli/v2/internal/config"
	runtimectx "github.com/getarcaneapp/arcane/cli/v2/internal/runtime"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"
)

// EffectiveLimit resolves the final list limit with precedence:
// explicit flag > per-resource config > global config > fallback default.
// A limit of ShowAllLimit is passed straight through so that `--limit -1`
// reaches the server as the documented "return everything" sentinel.
func EffectiveLimit(cmd *cobra.Command, resource, flagName string, flagValue, fallbackDefault int) int {
	if cmd != nil {
		if flag := cmd.Flags().Lookup(flagName); flag != nil && flag.Changed {
			return kit.Ternary(flagValue > 0 || flagValue == ShowAllLimit, flagValue, 0)
		}
	}

	resource = clitypes.NormalizePaginatedResource(resource)

	var ctx context.Context
	if cmd != nil {
		ctx = cmd.Context()
	}
	if app, ok := runtimectx.From(ctx); ok {
		if cfg := app.Config(); cfg != nil {
			if v := cfg.LimitFor(resource); v != 0 {
				return v
			}
		}
	} else if cfg, err := config.Load(); err == nil && cfg != nil {
		if v := cfg.LimitFor(resource); v != 0 {
			return v
		}
	}

	if fallbackDefault > 0 {
		return fallbackDefault
	}
	return kit.Ternary(flagValue > 0, flagValue, 0)
}
