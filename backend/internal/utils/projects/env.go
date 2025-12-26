package projects

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/dotenv"
)

const filePerm = 0600

const (
	globalEnvFileName  = ".env.global"
	projectEnvFileName = ".env"
	globalEnvHeader    = `# Global Environment Variables
# These variables are available to all projects
# Created: %s

`
)

type EnvMap map[string]string

type EnvLoader struct {
	projectsDir   string
	workdir       string
	autoInjectEnv bool
}

func NewEnvLoader(projectsDir, workdir string, autoInjectEnv bool) *EnvLoader {
	return &EnvLoader{
		projectsDir:   projectsDir,
		workdir:       workdir,
		autoInjectEnv: autoInjectEnv,
	}
}

// LoadEnvironment loads and merges environment variables from all sources:
// 1. Process environment
// 2. Global .env.global file (from projects directory)
// 3. Project-specific .env file (from workdir)
func (l *EnvLoader) LoadEnvironment(ctx context.Context) (envMap EnvMap, injectionVars EnvMap, err error) {
	envMap = l.loadProcessEnv()
	injectionVars = make(EnvMap)

	globalEnvPath := filepath.Join(l.projectsDir, globalEnvFileName)
	if err := l.ensureGlobalEnvFile(ctx, globalEnvPath); err != nil {
		slog.WarnContext(ctx, "Failed to ensure global env file", "path", globalEnvPath, "error", err)
	}

	if err := l.loadAndMergeGlobalEnv(ctx, globalEnvPath, envMap, injectionVars); err != nil {
		slog.WarnContext(ctx, "Failed to load global env", "path", globalEnvPath, "error", err)
	}

	projectEnvPath := filepath.Join(l.workdir, projectEnvFileName)
	if err := l.loadAndMergeProjectEnv(ctx, projectEnvPath, envMap, injectionVars); err != nil {
		slog.WarnContext(ctx, "Failed to load project env", "path", projectEnvPath, "error", err)
	}

	return envMap, injectionVars, nil
}

func (l *EnvLoader) loadProcessEnv() EnvMap {
	envMap := make(EnvMap)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			envMap[k] = v
		}
	}
	return envMap
}

func (l *EnvLoader) ensureGlobalEnvFile(ctx context.Context, path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		header := fmt.Sprintf(globalEnvHeader, time.Now().Format(time.RFC3339))
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dir: %w", err)
		}
		if werr := os.WriteFile(path, []byte(header), filePerm); werr != nil {
			return fmt.Errorf("write file: %w", werr)
		}
		slog.InfoContext(ctx, "Created global env file", "path", path)
	} else if err != nil {
		slog.DebugContext(ctx, "Could not stat global env file", "path", path, "error", err)
	}
	return nil
}

func (l *EnvLoader) loadAndMergeGlobalEnv(ctx context.Context, path string, envMap, injectionVars EnvMap) error {
	slog.DebugContext(ctx, "Checking for global env file", "path", path)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.DebugContext(ctx, "Global env file does not exist", "path", path)
		} else {
			slog.DebugContext(ctx, "Global env file not accessible", "path", path, "error", err)
		}
		return err
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", path)
	}

	slog.DebugContext(ctx, "Found global env file", "path", path)

	globalEnv, err := parseEnvFileWithContext(path, envMap)
	if err != nil {
		return fmt.Errorf("parse env file: %w", err)
	}

	slog.DebugContext(ctx, "Read global env file", "count", len(globalEnv))

	for k, v := range globalEnv {
		if _, exists := envMap[k]; !exists {
			envMap[k] = v
		}
		injectionVars[k] = v
	}

	slog.DebugContext(ctx, "Merged global env into environment map", "total_env_count", len(envMap))
	return nil
}

func (l *EnvLoader) loadAndMergeProjectEnv(ctx context.Context, path string, envMap, injectionVars EnvMap) error {
	slog.DebugContext(ctx, "Checking for project .env file", "path", path)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.DebugContext(ctx, "Project .env file does not exist", "path", path)
		} else {
			slog.DebugContext(ctx, "Project .env file not accessible", "path", path, "error", err)
		}
		return err
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", path)
	}

	slog.DebugContext(ctx, "Found project .env file", "path", path)

	projectEnv, err := parseEnvFileWithContext(path, envMap)
	if err != nil {
		return fmt.Errorf("parse env file: %w", err)
	}

	slog.DebugContext(ctx, "Read project .env file", "count", len(projectEnv))

	for k, v := range projectEnv {
		envMap[k] = v
		if l.autoInjectEnv {
			injectionVars[k] = v
		}
		slog.DebugContext(ctx, "Loaded env var from project .env", "key", k, "value", v)
	}

	slog.DebugContext(ctx, "Merged project .env into environment map", "total_env_count", len(envMap))
	return nil
}

// parseEnvFileWithContext parses an env file using compose-go's dotenv parser with variable expansion.
// The contextEnv map provides variables for expansion (e.g., from process env or previously loaded files).
// This handles ${VAR} syntax and proper quote handling automatically via compose-go's dotenv package.
func parseEnvFileWithContext(path string, contextEnv EnvMap) (EnvMap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	// Create lookup function for variable expansion
	// Checks contextEnv first (previously loaded vars), then process environment
	lookupFn := func(key string) (string, bool) {
		if val, ok := contextEnv[key]; ok {
			return val, true
		}
		return os.LookupEnv(key)
	}

	// Use compose-go's dotenv parser with lookup support for variable expansion
	envMap, err := dotenv.ParseWithLookup(f, lookupFn)
	if err != nil {
		return nil, fmt.Errorf("parse env file: %w", err)
	}

	return EnvMap(envMap), nil
}
