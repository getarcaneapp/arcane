package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/getarcaneapp/arcane/cli/v2/internal/config"
	runtimectx "github.com/getarcaneapp/arcane/cli/v2/internal/runtime"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"
)

const (
	// ShowAllLimit is the sentinel the Arcane API uses for "return everything".
	// The backend honours it on every list path: DB-backed lists short-circuit to
	// paginateDBAll (bypassing the 100-item clamp), in-memory Docker lists return
	// the full slice, and the handlers that coerce a zero limit to 20 test for
	// exactly 0 so they leave it alone.
	ShowAllLimit = -1

	// AllFlagUsage is the shared help text for the --all flag on list commands.
	AllFlagUsage = "Return every item, ignoring pagination"

	// StartFlagUsage is the shared help text for the --start flag on list commands.
	// Database-backed resources (projects, repos, gitops-syncs, environments,
	// registries, users, events, api-keys, roles) derive a page number from
	// start/limit, so an offset that is not a multiple of --limit is rounded down.
	StartFlagUsage = "Offset for pagination (rounded down to a multiple of --limit on database-backed resources)"
)

// AppendQuery merges extra query parameters into a path, preserving any that
// are already present.
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

// ListParams describes how a list command wants its results paginated.
type ListParams struct {
	// Resource is the canonical resource name used to look up a configured
	// per-resource limit. See clitypes.KnownPaginatedResources.
	Resource string
	// Limit is the value of the command's --limit flag.
	Limit int
	// FallbackDefault applies when neither the flag nor config supplies a limit.
	FallbackDefault int
	// Start is the value of the command's --start flag.
	Start int
	// All requests every item, ignoring pagination entirely.
	All bool
}

// ApplyPaginationParams appends limit and start query params to a list path.
// It preserves any existing query parameters on the path.
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
