package startup

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockSettingsPruner struct {
	pruneCalled bool
	pruneErr    error
}

func (m *mockSettingsPruner) PruneUnknownSettings(ctx context.Context) error {
	m.pruneCalled = true
	return m.pruneErr
}

func TestCleanupUnknownSettings(t *testing.T) {
	ctx := t.Context()

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
	ctx := t.Context()

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
	ctx := t.Context()

	t.Run("skips in agent mode", func(t *testing.T) {
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

		InitializeNonAgentFeatures(ctx, true, createAdminFunc, reconcileDefaultAdminAPIKeyFunc, nil)

		assert.False(t, createAdminCalled)
		assert.False(t, reconcileDefaultAdminAPIKeyCalled)
	})

	t.Run("calls all functions in non-agent mode", func(t *testing.T) {
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

		InitializeNonAgentFeatures(ctx, false, createAdminFunc, reconcileDefaultAdminAPIKeyFunc, autoLoginInitFunc)

		assert.True(t, createAdminCalled)
		assert.True(t, reconcileDefaultAdminAPIKeyCalled)
		assert.True(t, autoLoginInitCalled)
	})

	t.Run("handles errors gracefully", func(t *testing.T) {
		reconcileDefaultAdminAPIKeyFunc := func(ctx context.Context) error {
			return errors.New("api key reconcile failed")
		}
		autoLoginInitFunc := func(ctx context.Context) error {
			return errors.New("autologin init failed")
		}
		createAdminFunc := func(ctx context.Context) error {
			return errors.New("admin creation failed")
		}

		InitializeNonAgentFeatures(ctx, false, createAdminFunc, reconcileDefaultAdminAPIKeyFunc, autoLoginInitFunc)
	})
}
