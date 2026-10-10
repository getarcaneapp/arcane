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

// NormalizeSixField validates and normalizes a six-field cron schedule.
func NormalizeSixField(value, subject string) (string, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "", fmt.Errorf("%s schedule is required", subject)
	}
	if len(fields) != 6 {
		return "", fmt.Errorf("invalid %s schedule %q: expected six fields", subject, strings.TrimSpace(value))
	}
	normalized := strings.Join(fields, " ")
	parser := Parser()
	if _, err := parser.Parse(normalized); err != nil {
		return "", fmt.Errorf("invalid %s schedule %q: %w", subject, normalized, err)
	}
	return normalized, nil
}

// Or returns spec when it parses and fallback otherwise; job names the schedule in the warning.
func Or(ctx context.Context, spec, fallback, job string) string {
	spec = cmp.Or(spec, fallback)
	if _, err := Parser().Parse(spec); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression, using default", "job", job, "invalidSchedule", spec, "error", err)
		return fallback
	}
	return spec
}

// Parser is shared by schedule validation, execution, and next-run display.
func Parser() cron.Parser {
	return cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
}
