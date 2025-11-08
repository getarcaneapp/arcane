package cron

import (
	"fmt"

	"github.com/robfig/cron/v3"
)

// ValidateCronExpression validates a cron expression using robfig/cron parser
func ValidateCronExpression(expr string) error {
	if expr == "" {
		return nil // Empty is valid (means immediate updates)
	}

	// Create a parser that accepts standard 5-field cron expressions
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

	// Try to parse the expression
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	return nil
}
