package actors

import (
	"context"
	"sync"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func TestActorStateMapSerializesMutationsAndPublishesSnapshotsInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	state, err := NewActorStateMap[string, int](t.Context(), runtime, "state-map-test", "values", 1)
	require.NoError(t, err)

	require.NoError(t, state.Apply(t.Context(), "store value", func(values map[string]int) (bool, error) {
		values["answer"] = 42
		return true, nil
	}))
	value, ok := state.Get("answer")
	require.True(t, ok)
	require.Equal(t, 42, value)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, state.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestActorStateMapPublishesPartialMutationBeforeReturningErrorInternal(t *testing.T) {
	state := NewStateMap[string, int]()
	require.NoError(t, state.Apply(t.Context(), "seed", func(values map[string]int) (bool, error) {
		values["removed"] = 1
		values["retained"] = 2
		return true, nil
	}))

	expectedErr := errors.New("mutation failed after change")
	err := state.Apply(t.Context(), "partial mutation", func(values map[string]int) (bool, error) {
		delete(values, "removed")
		return true, expectedErr
	})
	require.ErrorIs(t, err, expectedErr)
	_, found := state.Get("removed")
	require.False(t, found)
	retained, found := state.Get("retained")
	require.True(t, found)
	require.Equal(t, 2, retained)
}

func TestStateMapPublishesPartialMutationBeforePropagatingPanicInternal(t *testing.T) {
	state := NewStateMap[string, int]()
	require.NoError(t, state.Apply(t.Context(), "seed", func(values map[string]int) (bool, error) {
		values["removed"] = 1
		return true, nil
	}))

	require.Panics(t, func() {
		_ = state.Apply(t.Context(), "panic after mutation", func(values map[string]int) (bool, error) {
			delete(values, "removed")
			panic("mutation panic")
		})
	})
	_, found := state.Get("removed")
	require.False(t, found)
}

func TestActorStateMapCanceledMutationDoesNotShareResultStorageInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	state, err := NewActorStateMap[string, int](t.Context(), runtime, "state-map-test", "canceled-result", 1)
	require.NoError(t, err)
	require.NoError(t, state.Apply(t.Context(), "seed", func(values map[string]int) (bool, error) {
		values["first"] = 1
		values["second"] = 2
		return true, nil
	}))

	mutationStarted := make(chan struct{})
	releaseMutation := make(chan struct{})
	var started sync.Once
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := make(chan []int, 1)
	resultErr := make(chan error, 1)
	go func() {
		removed, removeErr := state.RemoveWhere(ctx, "blocked removal", func(string, int) bool {
			started.Do(func() { close(mutationStarted) })
			<-releaseMutation
			return true
		})
		result <- removed
		resultErr <- removeErr
	}()

	<-mutationStarted
	require.ErrorIs(t, <-resultErr, context.DeadlineExceeded)
	require.Empty(t, <-result)
	close(releaseMutation)
	require.Eventually(t, func() bool {
		return len(state.Values()) == 0
	}, time.Second, time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, state.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}
