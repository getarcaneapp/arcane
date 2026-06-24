package startup

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockSettingsManager struct {
	persistCalled        bool
	setBoolCalled        bool
	ensureDefaultsCalled bool
	persistErr           error
	setBoolErr           error
	ensureDefaultsErr    error
}

func (m *mockSettingsManager) PersistEnvSettingsIfMissing(_ context.Context) error {
	m.persistCalled = true
	return m.persistErr
}

func (m *mockSettingsManager) SetBoolSetting(_ context.Context, _ string, _ bool) error {
	m.setBoolCalled = true
	return m.setBoolErr
}

func (m *mockSettingsManager) EnsureDefaultSettings(_ context.Context) error {
	m.ensureDefaultsCalled = true
	return m.ensureDefaultsErr
}

type mockSettingsPruner struct {
	pruneCalled bool
	pruneErr    error
}

func (m *mockSettingsPruner) PruneUnknownSettings(_ context.Context) error {
	m.pruneCalled = true
	return m.pruneErr
}

func TestLoadAgentToken(t *testing.T) {
	ctx := context.Background()

	t.Run("loads token when agent mode and token empty", func(t *testing.T) {
		cfg := &RuntimeConfig{
			AgentMode:  true,
			AgentToken: "",
		}
		getSettingFunc := func(ctx context.Context, key string, def string) string {
			if key == "agentToken" {
				return "loaded-token"
			}
			return def
		}

		LoadAgentToken(ctx, cfg, getSettingFunc)

		assert.Equal(t, "loaded-token", cfg.AgentToken)
	})

	t.Run("does not load when not in agent mode", func(t *testing.T) {
		cfg := &RuntimeConfig{
			AgentMode:  false,
			AgentToken: "",
		}
		called := false
		getSettingFunc := func(ctx context.Context, key string, def string) string {
			called = true
			return ""
		}

		LoadAgentToken(ctx, cfg, getSettingFunc)

		assert.False(t, called)
		assert.Empty(t, cfg.AgentToken)
	})

	t.Run("does not load when token already set", func(t *testing.T) {
		cfg := &RuntimeConfig{
			AgentMode:  true,
			AgentToken: "existing-token",
		}
		called := false
		getSettingFunc := func(ctx context.Context, key string, def string) string {
			called = true
			return ""
		}

		LoadAgentToken(ctx, cfg, getSettingFunc)

		assert.False(t, called)
		assert.Equal(t, "existing-token", cfg.AgentToken)
	})
}

func TestEnsureEncryptionKey(t *testing.T) {
	ctx := context.Background()

	t.Run("sets key when in agent mode", func(t *testing.T) {
		cfg := &RuntimeConfig{
			AgentMode:   true,
			Environment: "production",
		}
		ensureKeyFunc := func(ctx context.Context) (string, error) {
			return "secret-key", nil
		}

		EnsureEncryptionKey(ctx, cfg, ensureKeyFunc)

		assert.Equal(t, "secret-key", cfg.EncryptionKey)
	})

	t.Run("sets key when not in production", func(t *testing.T) {
		cfg := &RuntimeConfig{
			AgentMode:   false,
			Environment: "development",
		}
		ensureKeyFunc := func(ctx context.Context) (string, error) {
			return "dev-key", nil
		}

		EnsureEncryptionKey(ctx, cfg, ensureKeyFunc)

		assert.Equal(t, "dev-key", cfg.EncryptionKey)
	})

	t.Run("does not set key in production non-agent mode", func(t *testing.T) {
		cfg := &RuntimeConfig{
			AgentMode:     false,
			Environment:   "production",
			EncryptionKey: "existing",
		}
		called := false
		ensureKeyFunc := func(ctx context.Context) (string, error) {
			called = true
			return "new-key", nil
		}

		EnsureEncryptionKey(ctx, cfg, ensureKeyFunc)

		assert.False(t, called)
		assert.Equal(t, "existing", cfg.EncryptionKey)
	})

	t.Run("handles error gracefully", func(t *testing.T) {
		cfg := &RuntimeConfig{
			AgentMode:     true,
			EncryptionKey: "fallback",
		}
		ensureKeyFunc := func(ctx context.Context) (string, error) {
			return "", errors.New("key generation failed")
		}

		EnsureEncryptionKey(ctx, cfg, ensureKeyFunc)

		assert.Equal(t, "fallback", cfg.EncryptionKey)
	})
}

func TestInitializeDefaultSettings(t *testing.T) {
	ctx := context.Background()

	t.Run("calls all initialization methods", func(t *testing.T) {
		mgr := &mockSettingsManager{}

		InitializeDefaultSettings(ctx, mgr)

		assert.True(t, mgr.ensureDefaultsCalled)
		assert.True(t, mgr.persistCalled)
	})

	t.Run("handles ensure defaults error", func(t *testing.T) {
		mgr := &mockSettingsManager{
			ensureDefaultsErr: errors.New("defaults failed"),
		}

		InitializeDefaultSettings(ctx, mgr)

		assert.True(t, mgr.ensureDefaultsCalled)
		assert.True(t, mgr.persistCalled)
	})

	t.Run("handles persist error", func(t *testing.T) {
		mgr := &mockSettingsManager{
			persistErr: errors.New("persist failed"),
		}

		InitializeDefaultSettings(ctx, mgr)

		assert.True(t, mgr.ensureDefaultsCalled)
		assert.True(t, mgr.persistCalled)
	})
}

func TestCleanupUnknownSettings(t *testing.T) {
	ctx := context.Background()

	t.Run("calls prune method", func(t *testing.T) {
		pruner := &mockSettingsPruner{}

		CleanupUnknownSettings(ctx, pruner)

		assert.True(t, pruner.pruneCalled)
	})

	t.Run("handles error gracefully", func(t *testing.T) {
		pruner := &mockSettingsPruner{
			pruneErr: errors.New("prune failed"),
		}

		CleanupUnknownSettings(ctx, pruner)

		assert.True(t, pruner.pruneCalled)
	})
}

func TestTestDockerConnection(t *testing.T) {
	ctx := context.Background()

	t.Run("executes test function", func(t *testing.T) {
		called := false
		testFunc := func(ctx context.Context) error {
			called = true
			return nil
		}

		TestDockerConnection(ctx, testFunc)

		assert.True(t, called)
	})

	t.Run("handles error gracefully", func(t *testing.T) {
		testFunc := func(ctx context.Context) error {
			return errors.New("docker connection failed")
		}

		TestDockerConnection(ctx, testFunc)
	})
}

func TestInitializeNonAgentFeatures(t *testing.T) {
	ctx := context.Background()

	t.Run("skips in agent mode", func(t *testing.T) {
		cfg := &RuntimeConfig{AgentMode: true}
		createAdminCalled := false
		reconcileDefaultAdminAPIKeyCalled := false
		createAdminFunc := func(ctx context.Context) error {
			createAdminCalled = true
			return nil
		}
		reconcileDefaultAdminAPIKeyFunc := func(ctx context.Context) error {
			reconcileDefaultAdminAPIKeyCalled = true
			return nil
		}

		InitializeNonAgentFeatures(ctx, cfg, nil, createAdminFunc, reconcileDefaultAdminAPIKeyFunc, nil)

		assert.False(t, createAdminCalled)
		assert.False(t, reconcileDefaultAdminAPIKeyCalled)
	})

	t.Run("calls all functions in non-agent mode", func(t *testing.T) {
		cfg := &RuntimeConfig{AgentMode: false}
		createAdminCalled := false
		reconcileDefaultAdminAPIKeyCalled := false
		autoLoginInitCalled := false

		createAdminFunc := func(ctx context.Context) error {
			createAdminCalled = true
			return nil
		}
		reconcileDefaultAdminAPIKeyFunc := func(ctx context.Context) error {
			reconcileDefaultAdminAPIKeyCalled = true
			return nil
		}
		autoLoginInitFunc := func(ctx context.Context) error {
			autoLoginInitCalled = true
			return nil
		}

		InitializeNonAgentFeatures(ctx, cfg, nil, createAdminFunc, reconcileDefaultAdminAPIKeyFunc, autoLoginInitFunc)

		assert.True(t, createAdminCalled)
		assert.True(t, reconcileDefaultAdminAPIKeyCalled)
		assert.True(t, autoLoginInitCalled)
	})

	t.Run("handles errors gracefully", func(t *testing.T) {
		cfg := &RuntimeConfig{AgentMode: false}
		reconcileDefaultAdminAPIKeyFunc := func(ctx context.Context) error {
			return errors.New("api key reconcile failed")
		}
		autoLoginInitFunc := func(ctx context.Context) error {
			return errors.New("autologin init failed")
		}
		createAdminFunc := func(ctx context.Context) error {
			return errors.New("admin creation failed")
		}

		InitializeNonAgentFeatures(ctx, cfg, nil, createAdminFunc, reconcileDefaultAdminAPIKeyFunc, autoLoginInitFunc)
	})
}
