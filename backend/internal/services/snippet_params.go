package services

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	snippettypes "github.com/getarcaneapp/arcane/types/v2/snippet"
)

// Snippet parameter limits and conventions. A parameter value is never
// concatenated into shell text — resolveSnippetParamsInternal returns a map
// that callers apply as process environment variables only (see
// HostShellService.RunScript / ScriptRequest.Env). That is what makes the
// values below injection-safe regardless of content: execve environment
// entries are opaque byte strings the shell never re-parses.
const (
	snippetMaxParams          = 32
	snippetParamValueMaxBytes = 4096
)

// snippetParamNameRegex mirrors POSIX shell identifier syntax, matching the
// env-var-safe names lifecycle hooks already require (lifecycleEnvKeyRegex).
var snippetParamNameRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// snippetDeniedParamNames blocks names that would let a parameter override
// process-wide behavior (PATH/loader hijack, shell startup files) rather
// than just supply a script input. Any ARCANE_* name is denied too via
// isSnippetParamNameDeniedInternal.
var snippetDeniedParamNames = map[string]struct{}{
	"PATH":            {},
	"HOME":            {},
	"IFS":             {},
	"SHELL":           {},
	"ENV":             {},
	"BASH_ENV":        {},
	"PS4":             {},
	"LD_PRELOAD":      {},
	"LD_LIBRARY_PATH": {},
}

func isSnippetParamNameDeniedInternal(name string) bool {
	if _, denied := snippetDeniedParamNames[name]; denied {
		return true
	}
	return strings.HasPrefix(name, "ARCANE_")
}

// validateSnippetParameterDefsInternal validates a full parameter
// declaration list at snippet create/update time. Pure; no I/O.
func validateSnippetParameterDefsInternal(defs []snippettypes.ParameterDef) error {
	if len(defs) > snippetMaxParams {
		return common.Classify(common.ErrValidation, errors.Errorf("at most %d parameters are allowed", snippetMaxParams))
	}

	seen := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		if _, dup := seen[def.Name]; dup {
			return common.Classify(common.ErrValidation, errors.Errorf("duplicate parameter name %q", def.Name))
		}
		seen[def.Name] = struct{}{}

		if err := validateSnippetParameterDefInternal(def); err != nil {
			return err
		}
	}
	return nil
}

func validateSnippetParameterDefInternal(def snippettypes.ParameterDef) error {
	if !snippetParamNameRegex.MatchString(def.Name) {
		return common.Classify(common.ErrValidation, errors.Errorf("invalid parameter name %q", def.Name))
	}
	if isSnippetParamNameDeniedInternal(def.Name) {
		return common.Classify(common.ErrValidation, errors.Errorf("parameter name %q is reserved", def.Name))
	}

	switch def.Type {
	case snippettypes.ParameterTypeString, snippettypes.ParameterTypeNumber, snippettypes.ParameterTypeBoolean, snippettypes.ParameterTypeSelect:
	default:
		return common.Classify(common.ErrValidation, errors.Errorf("parameter %q: unknown type %q", def.Name, def.Type))
	}

	switch {
	case def.Type == snippettypes.ParameterTypeSelect && len(def.Options) == 0:
		return common.Classify(common.ErrValidation, errors.Errorf("parameter %q: select requires at least one option", def.Name))
	case def.Type != snippettypes.ParameterTypeSelect && len(def.Options) > 0:
		return common.Classify(common.ErrValidation, errors.Errorf("parameter %q: options are only valid for select parameters", def.Name))
	}

	if def.Pattern != "" {
		if def.Type != snippettypes.ParameterTypeString {
			return common.Classify(common.ErrValidation, errors.Errorf("parameter %q: pattern is only valid for string parameters", def.Name))
		}
		if _, err := regexp.Compile(def.Pattern); err != nil {
			return common.Classify(common.ErrValidation, errors.WrapIff(err, "parameter %q: invalid pattern", def.Name))
		}
	}

	if def.Default != "" {
		if err := validateSnippetParamValueInternal(def, def.Default); err != nil {
			return errors.WrapIff(err, "parameter %q: invalid default", def.Name)
		}
	}

	return nil
}

// validateSnippetParamValueInternal checks a non-empty resolved value
// against one parameter's type/pattern/options rule. Pure.
func validateSnippetParamValueInternal(def snippettypes.ParameterDef, value string) error {
	switch def.Type {
	case snippettypes.ParameterTypeNumber:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return common.Classify(common.ErrValidation, errors.Errorf("parameter %q must be a number", def.Name))
		}
	case snippettypes.ParameterTypeBoolean:
		if value != "true" && value != "false" {
			return common.Classify(common.ErrValidation, errors.Errorf("parameter %q must be \"true\" or \"false\"", def.Name))
		}
	case snippettypes.ParameterTypeSelect:
		if !slices.Contains(def.Options, value) {
			return common.Classify(common.ErrValidation, errors.Errorf("parameter %q must be one of %v", def.Name, def.Options))
		}
	case snippettypes.ParameterTypeString:
		if def.Pattern != "" {
			matched, err := regexp.MatchString(def.Pattern, value)
			if err != nil {
				return common.Classify(common.ErrValidation, errors.WrapIff(err, "parameter %q: invalid pattern", def.Name))
			}
			if !matched {
				return common.Classify(common.ErrValidation, errors.Errorf("parameter %q does not match the required pattern", def.Name))
			}
		}
	}
	return nil
}

// resolveSnippetParamsInternal resolves supplied parameter values against a
// snippet's declared parameters for one run (manual or scheduled). Pure — no
// I/O, no Docker, no DB — so it is exercised directly by table tests. The
// returned map is the only sanctioned way a parameter value reaches a
// snippet run: callers must apply it as the exec process environment
// (ScriptRequest.Env) and must never fold it into script text, string
// concatenation, or a template substitution — doing so would reintroduce
// exactly the shell-injection bug this design avoids.
func resolveSnippetParamsInternal(defs []snippettypes.ParameterDef, supplied map[string]string) (map[string]string, error) {
	declared := make(map[string]snippettypes.ParameterDef, len(defs))
	for _, def := range defs {
		declared[def.Name] = def
	}

	for key := range supplied {
		if _, ok := declared[key]; !ok {
			return nil, common.Classify(common.ErrValidation, errors.Errorf("undeclared parameter %q", key))
		}
	}

	resolved := make(map[string]string, len(defs))
	for _, def := range defs {
		value, wasSupplied := supplied[def.Name]
		switch {
		case wasSupplied:
		case def.Default != "":
			value = def.Default
		default:
			value = ""
		}

		if def.Required && value == "" {
			return nil, common.Classify(common.ErrValidation, errors.Errorf("parameter %q is required", def.Name))
		}
		if len(value) > snippetParamValueMaxBytes {
			return nil, common.Classify(common.ErrValidation, errors.Errorf("parameter %q exceeds %d bytes", def.Name, snippetParamValueMaxBytes))
		}
		if strings.ContainsRune(value, 0) {
			return nil, common.Classify(common.ErrValidation, errors.Errorf("parameter %q must not contain a NUL byte", def.Name))
		}
		if value != "" {
			if err := validateSnippetParamValueInternal(def, value); err != nil {
				return nil, err
			}
		}

		resolved[def.Name] = value
	}

	return resolved, nil
}
