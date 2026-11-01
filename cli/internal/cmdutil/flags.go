package cmdutil

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type (
	// EnumValue implements a string flag restricted to the choices registered by EnumVarP.
	EnumValue[T ~string] struct {
		value   *T
		allowed []string
	}
)

// String returns the current flag value.
func (v *EnumValue[T]) String() string { return string(*v.value) }

// Type keeps enum flags compatible with pflag's GetString accessor.
func (v *EnumValue[T]) Type() string { return "string" }

// Set matches the allowed choices case-insensitively and rejects others without changing the destination.
func (v *EnumValue[T]) Set(value string) error {
	if value == "" {
		*v.value = ""
		return nil
	}
	idx := slices.IndexFunc(v.allowed, func(choice string) bool { return strings.EqualFold(choice, value) })
	if idx < 0 {
		return fmt.Errorf("invalid value %q (allowed: %s)", value, strings.Join(v.allowed, ", "))
	}
	*v.value = T(v.allowed[idx])
	return nil
}

// EnumVarP registers a validated enum flag with shell completion and appends the choices to usage.
// The empty value is always allowed as unset; invalid nonempty defaults panic.
func EnumVarP[T ~string](cmd *cobra.Command, flags *pflag.FlagSet, p *T, name, shorthand string, value T, usage string, allowed ...T) {
	choices := make([]string, len(allowed))
	for i, choice := range allowed {
		choices[i] = string(choice)
	}
	enum := &EnumValue[T]{value: p, allowed: choices}
	if setErr := enum.Set(string(value)); setErr != nil {
		panic(fmt.Sprintf("flag --%s: default %v", name, setErr))
	}
	listed := slices.DeleteFunc(slices.Clone(choices), func(choice string) bool { return choice == "" })
	flags.VarP(enum, name, shorthand, fmt.Sprintf("%s (%s)", usage, strings.Join(listed, ", ")))
	if completionErr := cmd.RegisterFlagCompletionFunc(name, cobra.FixedCompletions(listed, cobra.ShellCompDirectiveNoFileComp)); completionErr != nil {
		panic(fmt.Sprintf("flag --%s: %v", name, completionErr))
	}
}

// ExclusiveFlags rejects more than one changed flag set to a non-zero value for its type.
func ExclusiveFlags(cmd *cobra.Command, names ...string) error {
	active := make([]string, 0, len(names))
	for _, name := range names {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			return fmt.Errorf("unknown flag --%s", name)
		}
		zero := map[string]string{"bool": "false", "int": "0", "stringSlice": "[]", "stringArray": "[]"}[flag.Value.Type()]
		if flag.Changed && flag.Value.String() != zero {
			active = append(active, "--"+name)
		}
	}
	if len(active) > 1 {
		return fmt.Errorf("flags %s cannot be used together", strings.Join(active, " and "))
	}
	return nil
}
