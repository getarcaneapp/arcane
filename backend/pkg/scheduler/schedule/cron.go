// Package schedule provides reusable schedule validation.
package schedule

import (
	"strings"

	"emperror.dev/errors"

	"github.com/robfig/cron/v3"
)

// NormalizeSixField validates and normalizes a six-field cron schedule.
func NormalizeSixField(value, subject string) (string, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "", errors.Errorf("%s schedule is required", subject)
	}
	if len(fields) != 6 {
		return "", errors.Errorf("invalid %s schedule %q: expected six fields", subject, strings.TrimSpace(value))
	}
	normalized := strings.Join(fields, " ")
	parser := Parser()
	if _, err := parser.Parse(normalized); err != nil {
		return "", errors.WrapIff(err, "invalid %s schedule %q", subject, normalized)
	}
	return normalized, nil
}

// Parser is shared by schedule validation, execution, and next-run display.
func Parser() cron.Parser {
	return cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
}
