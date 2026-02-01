//go:build buildables

package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_AutoLoginCredentials(t *testing.T) {
	origUsername := os.Getenv("AUTO_LOGIN_USERNAME")
	origPassword := os.Getenv("AUTO_LOGIN_PASSWORD")

	defer func() {
		restoreEnv("AUTO_LOGIN_USERNAME", origUsername)
		restoreEnv("AUTO_LOGIN_PASSWORD", origPassword)
	}()

	t.Run("Default auto-login values", func(t *testing.T) {
		os.Unsetenv("AUTO_LOGIN_USERNAME")
		os.Unsetenv("AUTO_LOGIN_PASSWORD")

		cfg := Load()
		assert.Equal(t, "arcane", cfg.BuildablesConfig.AutoLoginUsername, "AutoLoginUsername should default to 'arcane'")
		assert.Equal(t, "arcane-admin", cfg.BuildablesConfig.AutoLoginPassword, "AutoLoginPassword should default to 'arcane-admin'")
	})

	t.Run("Auto-login credentials from env", func(t *testing.T) {
		os.Setenv("AUTO_LOGIN_USERNAME", "testuser")
		os.Setenv("AUTO_LOGIN_PASSWORD", "testpassword123")

		cfg := Load()
		assert.Equal(t, "testuser", cfg.BuildablesConfig.AutoLoginUsername)
		assert.Equal(t, "testpassword123", cfg.BuildablesConfig.AutoLoginPassword)
	})
}
