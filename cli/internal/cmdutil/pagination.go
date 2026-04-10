package cmdutil

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

// ApplyPaginationParams appends limit and start query params to a list path.
// It preserves any existing query parameters on the path.
func ApplyPaginationParams(
	cmd *cobra.Command,
	path string,
	resource string,
	limitFlagName string,
	limitFlagValue int,
	fallbackDefault int,
	startFlagName string,
	start int,
) (string, error) {
	parsed, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("failed to parse path: %w", err)
	}

	query := parsed.Query()
	effectiveLimit := EffectiveLimit(cmd, resource, limitFlagName, limitFlagValue, fallbackDefault)
	if effectiveLimit > 0 {
		query.Set("limit", strconv.Itoa(effectiveLimit))
	}
	if cmd != nil {
		if flag := cmd.Flags().Lookup(startFlagName); flag != nil && flag.Changed {
			query.Set("start", strconv.Itoa(start))
		}
	}

	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
