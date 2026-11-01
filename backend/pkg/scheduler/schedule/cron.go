// Package schedule provides reusable schedule validation.
package schedule

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/robfig/cron/v3"
)

func Normalize(value, subject string) (string, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "", fmt.Errorf("%s schedule is required", subject)
	}
	if !strings.HasPrefix(fields[0], "@") && len(fields) != 5 {
		return "", fmt.Errorf("invalid %s schedule %q: expected five fields", subject, strings.TrimSpace(value))
	}
	normalized := strings.Join(fields, " ")
	if _, err := cron.ParseStandard(normalized); err != nil {
		return "", fmt.Errorf("invalid %s schedule %q: %w", subject, normalized, err)
	}
	return normalized, nil
}

// Or returns spec when it parses and fallback otherwise; job names the schedule in the warning.
func Or(ctx context.Context, spec, fallback, job string) string {
	spec = cmp.Or(spec, fallback)
	if _, err := cron.ParseStandard(spec); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression, using default", "job", job, "invalidSchedule", spec, "error", err)
		return fallback
	}
	return spec
}
