// Package setupplan works out which variables a project needs, for the secret
// setup wizard, and edits .env content to drop moved keys. It is pure: callers
// read and write the files.
package setupplan

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"

	"getarcane.dev/types/v3"
)

type (
	// ComposeVariable summarizes every ${NAME} reference to one variable.
	ComposeVariable struct {
		// AllDefaulted is true when every reference has a default or alternate
		// value, such as ${NAME:-x} or ${NAME:+x}, so compose works without it.
		AllDefaulted bool
		// Required is true when a reference fails without it, such as ${NAME:?}.
		Required bool
	}

	// Inputs are the parsed files of one project.
	Inputs struct {
		// EnvValues are the parsed values of the project's effective .env.
		EnvValues map[string]string
		// GitKeys are keys that come from the Git repository's env file.
		GitKeys map[string]struct{}
		// ComposeFiles are the compose file contents, override files included.
		ComposeFiles []string
	}

	referenceInternal struct {
		name      string
		defaulted bool
		required  bool
	}
)

var (
	envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// secretWords mark names that usually hold credentials.
	secretWords = []string{"PASSWORD", "PASSWD", "SECRET", "TOKEN", "APIKEY", "API_KEY", "PRIVATE", "CREDENTIAL", "SALT", "DSN"}
	// secretSuffixes catch short forms such as DB_PASS or APP_KEY.
	secretSuffixes = []string{"_PASS", "_PW", "_PWD", "_KEY"}
)

// Analyze lists every variable the compose files reference or the .env
// defines, sorted by name. Compose's own settings (COMPOSE_*) are left out:
// Arcane reads them from the .env before any secret is fetched.
func Analyze(in Inputs) ([]types.SecretSourceSetupVariable, error) {
	compose := map[string]ComposeVariable{}
	for i, content := range in.ComposeFiles {
		vars, err := ComposeVariables(content)
		if err != nil {
			return nil, fmt.Errorf("compose file %d: %w", i+1, err)
		}
		for name, v := range vars {
			if existing, ok := compose[name]; ok {
				v.AllDefaulted = v.AllDefaulted && existing.AllDefaulted
				v.Required = v.Required || existing.Required
			}
			compose[name] = v
		}
	}

	seen := map[string]struct{}{}
	add := func(key string) {
		if envKeyPattern.MatchString(key) && !strings.HasPrefix(key, "COMPOSE_") {
			seen[key] = struct{}{}
		}
	}
	for key := range compose {
		add(key)
	}
	for key := range in.EnvValues {
		add(key)
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	variables := make([]types.SecretSourceSetupVariable, 0, len(keys))
	for _, key := range keys {
		ref, inCompose := compose[key]
		value, inEnv := in.EnvValues[key]
		_, fromGit := in.GitKeys[key]
		variables = append(variables, types.SecretSourceSetupVariable{
			Key:             key,
			InCompose:       inCompose,
			ComposeDefault:  inCompose && ref.AllDefaulted,
			ComposeRequired: inCompose && ref.Required,
			InEnvFile:       inEnv,
			EnvEmpty:        inEnv && value == "",
			FromGit:         fromGit,
			SecretLike:      IsSecretLike(key),
			Remote:          types.SecretSourceSetupRemoteUnknown,
		})
	}
	return variables, nil
}

// IsSecretLike reports whether a name looks like it holds a credential, such
// as DB_PASSWORD, GITHUB_TOKEN, or APP_KEY. Paths to secret files (*_FILE,
// *_PATH) are not secrets themselves.
func IsSecretLike(key string) bool {
	upper := strings.ToUpper(key)
	if strings.HasSuffix(upper, "_FILE") || strings.HasSuffix(upper, "_PATH") {
		return false
	}
	for _, word := range secretWords {
		if strings.Contains(upper, word) {
			return true
		}
	}
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return false
}

// ComposeVariables finds the ${NAME} and $NAME references in the values of one
// compose file. Keys are not interpolated by compose and are ignored, as are
// comments and escaped $$ references.
func ComposeVariables(content string) (map[string]ComposeVariable, error) {
	var doc any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	vars := map[string]ComposeVariable{}
	walkValuesInternal(doc, func(value string) {
		for _, ref := range scanReferencesInternal(value) {
			existing, ok := vars[ref.name]
			if !ok {
				existing = ComposeVariable{AllDefaulted: true}
			}
			existing.AllDefaulted = existing.AllDefaulted && ref.defaulted
			existing.Required = existing.Required || ref.required
			vars[ref.name] = existing
		}
	})
	return vars, nil
}

func walkValuesInternal(node any, visit func(string)) {
	switch value := node.(type) {
	case string:
		visit(value)
	case map[string]any:
		for _, child := range value {
			walkValuesInternal(child, visit)
		}
	case map[any]any:
		for _, child := range value {
			walkValuesInternal(child, visit)
		}
	case []any:
		for _, child := range value {
			walkValuesInternal(child, visit)
		}
	}
}

// scanReferencesInternal parses compose interpolation in one string value,
// including references nested in defaults, such as ${A:-${B}}.
func scanReferencesInternal(value string) []referenceInternal {
	refs := []referenceInternal{}
	for i := 0; i < len(value); i++ {
		if value[i] != '$' || i+1 >= len(value) {
			continue
		}
		next := value[i+1]
		switch {
		case next == '$':
			i++ // escaped $$
		case next == '{':
			end := closingBraceInternal(value, i+2)
			if end < 0 {
				return refs
			}
			refs = append(refs, parseBracedInternal(value[i+2:end])...)
			i = end
		case isNameStartInternal(next):
			j := i + 1
			for j < len(value) && isNameCharInternal(value[j]) {
				j++
			}
			refs = append(refs, referenceInternal{name: value[i+1 : j]})
			i = j - 1
		}
	}
	return refs
}

// parseBracedInternal parses the inside of ${...}.
func parseBracedInternal(inner string) []referenceInternal {
	j := 0
	for j < len(inner) && isNameCharInternal(inner[j]) {
		j++
	}
	name := inner[:j]
	if name == "" || !isNameStartInternal(name[0]) {
		return nil
	}
	ref := referenceInternal{name: name}
	rest := strings.TrimPrefix(inner[j:], ":")
	var nested string
	switch {
	case rest == "":
	case strings.HasPrefix(rest, "-"), strings.HasPrefix(rest, "+"):
		ref.defaulted = true
		nested = rest[1:]
	case strings.HasPrefix(rest, "?"):
		ref.required = true
	}
	refs := []referenceInternal{ref}
	if nested != "" {
		// A variable used only inside a default is optional too.
		for _, inner := range scanReferencesInternal(nested) {
			inner.defaulted = true
			refs = append(refs, inner)
		}
	}
	return refs
}

func closingBraceInternal(value string, start int) int {
	depth := 1
	for k := start; k < len(value); k++ {
		switch value[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return -1
}

func isNameStartInternal(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isNameCharInternal(c byte) bool {
	return isNameStartInternal(c) || (c >= '0' && c <= '9')
}
