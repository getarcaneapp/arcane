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
			projectEnv, parseValidationEnvContentErr := ParseProjectEnvContent(*effectiveEnvContent, fullEnvMap)
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

	return ParseProjectEnvContent(string(content), contextEnv)
}

// ParseEnvContent lists the variables declared in Compose env content.
//
// Each variable carries the name and the value Compose reads from its line: the
// `export` prefix is not part of the name, and an unquoted value ends at the
// comment Compose would drop there.
func ParseEnvContent(content string) []types.EnvVariable {
	if content == "" {
		return nil
	}

	var vars []types.EnvVariable
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		match := envKeyLineRegex.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		vars = append(vars, types.EnvVariable{
			Key:   match[2],
			Value: parseEnvContentValue(match[4]),
		})
	}

	return vars
}

// parseEnvContentValue reads one value the way Compose reads it: a quoted value
// ends at its closing quote, and an unquoted value ends at the comment a space
// followed by "#" starts.
func parseEnvContentValue(raw string) string {
	value := strings.TrimSpace(raw)
	if quote, quoted := envContentQuotePrefix(value); quoted {
		escaped := false
		for i := 1; i < len(value); i++ {
			switch {
			case escaped:
				escaped = false
			case value[i] == '\\':
				escaped = true
			case value[i] == quote:
				return strings.ReplaceAll(value[1:i], `\`+string(quote), string(quote))
			}
		}
		// An unterminated quote is not a value Compose would accept.
		return ""
	}

	value, _, _ = strings.Cut(value, " #")
	return strings.TrimSpace(value)
}

func envContentQuotePrefix(value string) (byte, bool) {
	if value == "" {
		return 0, false
	}
	switch value[0] {
	case '"', '\'':
		return value[0], true
	default:
		return 0, false
	}
}
