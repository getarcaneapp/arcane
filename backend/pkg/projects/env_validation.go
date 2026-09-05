package projects

import (
	"bufio"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
	"github.com/compose-spec/compose-go/v2/consts"
	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/getarcaneapp/arcane/types/v2/env"
)

// BuildValidationEnvironment resolves the env a compose file is validated
// against. It matches the project-visible env sources without inheriting the
// Arcane process environment, which may contain unrelated secrets.
func BuildValidationEnvironment(ctx context.Context, projectsDirectory, projectPath string, effectiveEnvContent *string) (EnvMap, error) {
	fullEnvMap := make(EnvMap)
	if absWorkdir, absErr := filepath.Abs(projectPath); absErr == nil {
		fullEnvMap["PWD"] = absWorkdir
	} else {
		fullEnvMap["PWD"] = projectPath
	}

	globalEnvPath := filepath.Join(projectsDirectory, GlobalEnvFileName)
	globalEnv, err := ParseValidationEnvFile(globalEnvPath, fullEnvMap)
	if err != nil {
		return nil, errors.WrapIf(err, "parse global env file")
	}
	maps.Copy(fullEnvMap, globalEnv)

	// Mirror LoadEnvironment's COMPOSE_DISABLE_ENV_FILE / COMPOSE_ENV_FILES
	// handling so the validation env matches the runtime env exactly.
	if !parseComposeBoolInternal(fullEnvMap, consts.ComposeDisableDefaultEnvFile) {
		if effectiveEnvContent != nil {
			projectEnv, err := ParseValidationEnvContent(*effectiveEnvContent, fullEnvMap)
			if err != nil {
				return nil, errors.WrapIf(err, "parse provided env content")
			}
			maps.Copy(fullEnvMap, projectEnv)
		} else {
			projectEnv, err := ParseValidationEnvFile(filepath.Join(projectPath, ".env"), fullEnvMap)
			if err != nil {
				return nil, errors.WrapIf(err, "parse project env file")
			}
			maps.Copy(fullEnvMap, projectEnv)
		}
	}

	mergeComposeEnvFilesInternal(ctx, projectPath, fullEnvMap, ParseValidationEnvFile, nil)

	return fullEnvMap, nil
}

// ParseValidationEnvFile parses one env file against contextEnv. A missing file
// is not an error.
//
// Stays on os.*: env files may be symlinks resolving outside any confinement
// root (a supported setup), which acfs cannot follow.
func ParseValidationEnvFile(path string, contextEnv EnvMap) (EnvMap, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.WrapIf(err, "stat file")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.WrapIf(err, "read file")
	}

	return ParseValidationEnvContent(string(content), contextEnv)
}

// ParseValidationEnvContent parses env content, resolving interpolations
// against contextEnv rather than the process environment.
func ParseValidationEnvContent(content string, contextEnv EnvMap) (EnvMap, error) {
	lookupFn := func(key string) (string, bool) {
		value, ok := contextEnv[key]
		return value, ok
	}

	envMap, err := dotenv.ParseWithLookup(strings.NewReader(content), lookupFn)
	if err != nil {
		return nil, errors.WrapIf(err, "parse env")
	}

	return envMap, nil
}

// ParseEnvContent parses environment variables from .env file content
func ParseEnvContent(content string) []env.Variable {
	if content == "" {
		return []env.Variable{}
	}

	var vars []env.Variable
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on first '='
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := ""
		if len(parts) == 2 {
			value = strings.TrimSpace(parts[1])
		}

		// Strip surrounding quotes and handle escapes
		if len(value) >= 2 {
			if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
				value = value[1 : len(value)-1]
				value = strings.ReplaceAll(value, `\"`, `"`)
			} else if strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`) {
				value = value[1 : len(value)-1]
				value = strings.ReplaceAll(value, `\'`, `'`)
			}
		}

		if key != "" {
			vars = append(vars, env.Variable{
				Key:   key,
				Value: value,
			})
		}
	}

	return vars
}
