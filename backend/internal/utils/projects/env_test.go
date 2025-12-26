package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEnvironment(t *testing.T) {
	// Setup temp dirs
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects")
	workdir := filepath.Join(projectsDir, "myproject")

	err := os.MkdirAll(workdir, 0755)
	require.NoError(t, err)

	// Create .env.global
	globalEnvContent := "GLOBAL_VAR=global_value\nSHARED_VAR=global_shared"
	err = os.WriteFile(filepath.Join(projectsDir, ".env.global"), []byte(globalEnvContent), 0600)
	require.NoError(t, err)

	// Create .env
	projectEnvContent := "PROJECT_VAR=project_value\nSHARED_VAR=project_shared"
	err = os.WriteFile(filepath.Join(workdir, ".env"), []byte(projectEnvContent), 0600)
	require.NoError(t, err)

	t.Run("AutoInjectEnv=false", func(t *testing.T) {
		loader := NewEnvLoader(projectsDir, workdir, false)
		ctx := context.Background()

		envMap, injectionVars, err := loader.LoadEnvironment(ctx)
		require.NoError(t, err)

		// Verify envMap (should contain all vars, project overrides global)
		assert.Equal(t, "global_value", envMap["GLOBAL_VAR"])
		assert.Equal(t, "project_value", envMap["PROJECT_VAR"])
		assert.Equal(t, "project_shared", envMap["SHARED_VAR"])

		// Verify injectionVars (should ONLY contain global vars)
		assert.Equal(t, "global_value", injectionVars["GLOBAL_VAR"])
		assert.Equal(t, "global_shared", injectionVars["SHARED_VAR"])

		_, projectVarInInjection := injectionVars["PROJECT_VAR"]
		assert.False(t, projectVarInInjection, "Project variable should not be in injectionVars")
	})

	t.Run("AutoInjectEnv=true", func(t *testing.T) {
		loader := NewEnvLoader(projectsDir, workdir, true)
		ctx := context.Background()

		envMap, injectionVars, err := loader.LoadEnvironment(ctx)
		require.NoError(t, err)

		// Verify envMap
		assert.Equal(t, "global_value", envMap["GLOBAL_VAR"])
		assert.Equal(t, "project_value", envMap["PROJECT_VAR"])
		assert.Equal(t, "project_shared", envMap["SHARED_VAR"])

		// Verify injectionVars (should contain both global and project vars)
		assert.Equal(t, "global_value", injectionVars["GLOBAL_VAR"])
		assert.Equal(t, "project_value", injectionVars["PROJECT_VAR"])
		assert.Equal(t, "project_shared", injectionVars["SHARED_VAR"])
	})
}
