package cli

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// NoArgs validates args and returns an error if there are any args
func NoArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}

	if cmd.HasSubCommands() {
		return errors.Errorf(
			"%[1]s: unknown command: %[2]s %[3]s\n\nUsage:  %[4]s\n\nRun '%[2]s --help' for more information",
			binName(cmd),
			cmd.CommandPath(),
			args[0],
			cmd.UseLine(),
		)
	}

	return errors.Errorf(
		"%[1]s: '%[2]s' accepts no arguments\n\nUsage:  %[3]s\n\nRun '%[2]s --help' for more information",
		binName(cmd),
		cmd.CommandPath(),
		cmd.UseLine(),
	)
}

// RequiresMinArgs returns an error if there is not at least min args
func RequiresMinArgs(minArgs int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) >= minArgs {
			return nil
		}
		return errors.Errorf(
			"%[1]s: '%[2]s' requires at least %[3]d %[4]s\n\nUsage:  %[5]s\n\nSee '%[2]s --help' for more information",
			binName(cmd),
			cmd.CommandPath(),
			minArgs,
			pluralize("argument", minArgs),
			cmd.UseLine(),
		)
	}
}

// RequiresMaxArgs returns an error if there is not at most max args
func RequiresMaxArgs(maxArgs int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) <= maxArgs {
			return nil
		}
		return errors.Errorf(
			"%[1]s: '%[2]s' requires at most %[3]d %[4]s\n\nUsage:  %[5]s\n\nSRun '%[2]s --help' for more information",
			binName(cmd),
			cmd.CommandPath(),
			maxArgs,
			pluralize("argument", maxArgs),
			cmd.UseLine(),
		)
	}
}

// RequiresRangeArgs returns an error if there is not at least min args and at most max args
func RequiresRangeArgs(minArgs int, maxArgs int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) >= minArgs && len(args) <= maxArgs {
			return nil
		}
		return errors.Errorf(
			"%[1]s: '%[2]s' requires at least %[3]d and at most %[4]d %[5]s\n\nUsage:  %[6]s\n\nRun '%[2]s --help' for more information",
			binName(cmd),
			cmd.CommandPath(),
			minArgs,
			maxArgs,
			pluralize("argument", maxArgs),
			cmd.UseLine(),
		)
	}
}

// ExactArgs returns an error if there is not the exact number of args
func ExactArgs(number int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == number {
			return nil
		}
		return errors.Errorf(
			"%[1]s: '%[2]s' requires %[3]d %[4]s\n\nUsage:  %[5]s\n\nRun '%[2]s --help' for more information",
			binName(cmd),
			cmd.CommandPath(),
			number,
			pluralize("argument", number),
			cmd.UseLine(),
		)
	}
}

// binName returns the name of the binary / root command (usually 'docker').
func binName(cmd *cobra.Command) string {
	return cmd.Root().Name()
}

//nolint:unparam
func pluralize(word string, number int) string {
	if number == 1 {
		return word
	}
	return word + "s"
}
