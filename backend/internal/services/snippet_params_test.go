package services

import (
	"strings"
	"testing"

	"emperror.dev/errors"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	snippettypes "github.com/getarcaneapp/arcane/types/v2/snippet"
)

// TestResolveSnippetParamsInternal_InjectionSafe is the security core: a
// parameter value must survive resolution byte-for-byte and land in a map,
// never a string, regardless of how shell-hostile its content looks. There
// is no substitution pass to exploit because there is no substitution pass.
func TestResolveSnippetParamsInternal_InjectionSafe(t *testing.T) {
	defs := []snippettypes.ParameterDef{{Name: "FOO", Type: snippettypes.ParameterTypeString}}

	values := []string{
		"; rm -rf /",
		"$(whoami)",
		"`id`",
		"'; touch /pwned; '",
		"&& curl evil",
		"line1\nline2\nline3",
		"has \"double\" and 'single' quotes",
		"back\\slash\\path",
		"unicode: 日本語 emoji: 🔥",
	}

	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			resolved, err := resolveSnippetParamsInternal(defs, map[string]string{"FOO": value})
			require.NoError(t, err)
			require.IsType(t, map[string]string{}, resolved)
			require.Equal(t, value, resolved["FOO"], "value must survive byte-for-byte, unquoted, unescaped")
		})
	}
}

func TestResolveSnippetParamsInternal_DefaultsAndFallback(t *testing.T) {
	defs := []snippettypes.ParameterDef{
		{Name: "WITH_DEFAULT", Type: snippettypes.ParameterTypeString, Default: "fallback"},
		{Name: "NO_DEFAULT", Type: snippettypes.ParameterTypeString},
	}

	resolved, err := resolveSnippetParamsInternal(defs, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "fallback", resolved["WITH_DEFAULT"])
	require.Equal(t, "", resolved["NO_DEFAULT"])

	resolved, err = resolveSnippetParamsInternal(defs, map[string]string{"WITH_DEFAULT": "supplied"})
	require.NoError(t, err)
	require.Equal(t, "supplied", resolved["WITH_DEFAULT"])
}

func TestResolveSnippetParamsInternal_TypeChecks(t *testing.T) {
	numberDef := snippettypes.ParameterDef{Name: "N", Type: snippettypes.ParameterTypeNumber}
	boolDef := snippettypes.ParameterDef{Name: "B", Type: snippettypes.ParameterTypeBoolean}
	selectDef := snippettypes.ParameterDef{Name: "S", Type: snippettypes.ParameterTypeSelect, Options: []string{"a", "b"}}

	_, err := resolveSnippetParamsInternal([]snippettypes.ParameterDef{numberDef}, map[string]string{"N": "42.5"})
	require.NoError(t, err)
	_, err = resolveSnippetParamsInternal([]snippettypes.ParameterDef{numberDef}, map[string]string{"N": "not-a-number"})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)

	_, err = resolveSnippetParamsInternal([]snippettypes.ParameterDef{boolDef}, map[string]string{"B": "true"})
	require.NoError(t, err)
	_, err = resolveSnippetParamsInternal([]snippettypes.ParameterDef{boolDef}, map[string]string{"B": "yes"})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)

	_, err = resolveSnippetParamsInternal([]snippettypes.ParameterDef{selectDef}, map[string]string{"S": "a"})
	require.NoError(t, err)
	_, err = resolveSnippetParamsInternal([]snippettypes.ParameterDef{selectDef}, map[string]string{"S": "c"})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrValidation)
}

func TestResolveSnippetParamsInternal_Rejections(t *testing.T) {
	defs := []snippettypes.ParameterDef{
		{Name: "REQ", Type: snippettypes.ParameterTypeString, Required: true},
		{Name: "OPT", Type: snippettypes.ParameterTypeString},
	}

	tests := []struct {
		name     string
		supplied map[string]string
	}{
		{"value exceeds 4096 bytes", map[string]string{"REQ": "x", "OPT": strings.Repeat("a", 4097)}},
		{"NUL byte in value", map[string]string{"REQ": "x", "OPT": "bad\x00value"}},
		{"undeclared supplied key", map[string]string{"REQ": "x", "NOPE": "y"}},
		{"required and empty", map[string]string{"REQ": ""}},
		{"required missing entirely", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveSnippetParamsInternal(defs, tt.supplied)
			require.Error(t, err)
			require.ErrorIs(t, err, common.ErrValidation)
		})
	}
}

func TestValidateSnippetParameterDefsInternal_Accepts(t *testing.T) {
	defs := []snippettypes.ParameterDef{
		{Name: "CONTAINER", Type: snippettypes.ParameterTypeString, Pattern: `^[a-z0-9_-]+$`},
		{Name: "count", Type: snippettypes.ParameterTypeNumber, Default: "3"},
		{Name: "_enabled", Type: snippettypes.ParameterTypeBoolean, Default: "true"},
		{Name: "MODE", Type: snippettypes.ParameterTypeSelect, Options: []string{"fast", "slow"}, Default: "fast"},
	}
	require.NoError(t, validateSnippetParameterDefsInternal(defs))
}

func TestValidateSnippetParameterDefsInternal_Rejections(t *testing.T) {
	tests := []struct {
		name string
		defs []snippettypes.ParameterDef
	}{
		{
			"too many parameters",
			func() []snippettypes.ParameterDef {
				defs := make([]snippettypes.ParameterDef, 33)
				for i := range defs {
					defs[i] = snippettypes.ParameterDef{Name: "P" + string(rune('A'+i)), Type: snippettypes.ParameterTypeString}
				}
				return defs
			}(),
		},
		{"invalid name", []snippettypes.ParameterDef{{Name: "1BAD", Type: snippettypes.ParameterTypeString}}},
		{"empty name", []snippettypes.ParameterDef{{Name: "", Type: snippettypes.ParameterTypeString}}},
		{"denied name PATH", []snippettypes.ParameterDef{{Name: "PATH", Type: snippettypes.ParameterTypeString}}},
		{"denied name LD_PRELOAD", []snippettypes.ParameterDef{{Name: "LD_PRELOAD", Type: snippettypes.ParameterTypeString}}},
		{"denied ARCANE_ prefix", []snippettypes.ParameterDef{{Name: "ARCANE_SECRET", Type: snippettypes.ParameterTypeString}}},
		{
			"duplicate names",
			[]snippettypes.ParameterDef{
				{Name: "DUP", Type: snippettypes.ParameterTypeString},
				{Name: "DUP", Type: snippettypes.ParameterTypeString},
			},
		},
		{"select without options", []snippettypes.ParameterDef{{Name: "S", Type: snippettypes.ParameterTypeSelect}}},
		{"options on non-select", []snippettypes.ParameterDef{{Name: "S", Type: snippettypes.ParameterTypeString, Options: []string{"a"}}}},
		{"pattern on non-string", []snippettypes.ParameterDef{{Name: "N", Type: snippettypes.ParameterTypeNumber, Pattern: "^[0-9]+$"}}},
		{"uncompilable pattern", []snippettypes.ParameterDef{{Name: "S", Type: snippettypes.ParameterTypeString, Pattern: "(["}}},
		{"non-numeric number default", []snippettypes.ParameterDef{{Name: "N", Type: snippettypes.ParameterTypeNumber, Default: "abc"}}},
		{"out-of-set select default", []snippettypes.ParameterDef{{Name: "S", Type: snippettypes.ParameterTypeSelect, Options: []string{"a", "b"}, Default: "c"}}},
		{"boolean default not true/false", []snippettypes.ParameterDef{{Name: "B", Type: snippettypes.ParameterTypeBoolean, Default: "yes"}}},
		{"unknown type", []snippettypes.ParameterDef{{Name: "X", Type: "float"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSnippetParameterDefsInternal(tt.defs)
			require.Error(t, err)
			require.True(t, errors.Is(err, common.ErrValidation), "expected ErrValidation, got %v", err)
		})
	}
}
