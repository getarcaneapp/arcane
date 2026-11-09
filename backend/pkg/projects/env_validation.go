package projects

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/consts"
	"github.com/compose-spec/compose-go/v2/dotenv"

	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/types/v3"
)

func BuildValidationEnvironment(ctx context.Context, projectsDirectory, projectPath string, effectiveEnvContent *string) (EnvMap, error) {
	fullEnvMap := make(EnvMap)
	if absWorkdir, absErr := filepath.Abs(projectPath); absErr == nil {
		fullEnvMap["PWD"] = absWorkdir
	} else {
		fullEnvMap["PWD"] = projectPath
	}

	globalEnvPath := filepath.Join(projectsDirectory, GlobalEnvFileName)
	globalEnv, err := ParseValidationEnvFile(ctx, globalEnvPath, fullEnvMap)
	if err != nil {
		return nil, fmt.Errorf("parse global env file: %w", err)
	}
	maps.Copy(fullEnvMap, globalEnv)

	// Mirror LoadEnvironment's COMPOSE_DISABLE_ENV_FILE / COMPOSE_ENV_FILES
	// handling so the validation env matches the runtime env exactly.
	if !parseComposeBool(fullEnvMap, consts.ComposeDisableDefaultEnvFile) {
		if effectiveEnvContent != nil {
			projectEnv, parseValidationEnvContentErr := ParseValidationEnvContent(*effectiveEnvContent, fullEnvMap)
			if parseValidationEnvContentErr != nil {
				return nil, fmt.Errorf("parse provided env content: %w", parseValidationEnvContentErr)
			}
			maps.Copy(fullEnvMap, projectEnv)
		} else {
			projectEnv, parseValidationEnvFileErr := ParseValidationEnvFile(ctx, filepath.Join(projectPath, ".env"), fullEnvMap)
			if parseValidationEnvFileErr != nil {
				return nil, fmt.Errorf("parse project env file: %w", parseValidationEnvFileErr)
			}
			maps.Copy(fullEnvMap, projectEnv)
		}
	}

	mergeComposeEnvFiles(ctx, projectPath, fullEnvMap, ParseValidationEnvFile, nil)

	return fullEnvMap, nil
}

func ParseValidationEnvFile(ctx context.Context, path string, contextEnv EnvMap) (EnvMap, error) {
	info, err := acfs.Host{}.Stat(ctx, path, acfstypes.StatOptions{FollowFinal: true})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat file: %w", err)
	}
	if info.IsDirectory {
		return nil, nil
	}

	content, err := acfs.Host{}.ReadFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return ParseValidationEnvContent(string(content), contextEnv)
}

func ParseValidationEnvContent(content string, contextEnv EnvMap) (EnvMap, error) {
	lookupFn := func(key string) (string, bool) {
		value, ok := contextEnv[key]
		return value, ok
	}

	envMap, err := dotenv.ParseWithLookup(strings.NewReader(content), lookupFn)
	if err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}

	return envMap, nil
}

func ParseEnvContent(content string) []types.EnvVariable {
	if content == "" {
		return []types.EnvVariable{}
	}

	var vars []types.EnvVariable
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := ""
		if len(parts) == 2 {
			value = strings.TrimSpace(parts[1])
		}

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
			vars = append(vars, types.EnvVariable{
				Key:   key,
				Value: value,
			})
		}
	}

	return vars
}
