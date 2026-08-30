package projects

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
	"github.com/compose-spec/compose-go/v2/consts"
	"github.com/compose-spec/compose-go/v2/dotenv"
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
