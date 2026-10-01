package concurrency

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStateMapSerializesMutationsAndPublishesSnapshotsInternal(t *testing.T) {
	state := NewStateMap[string, int]()

	require.NoError(t, state.Apply(t.Context(), "store value", func(values map[string]int) (bool, error) {
		values["answer"] = 42
		return true, nil
	}))
	value, ok := state.Get("answer")
	require.True(t, ok)
	require.Equal(t, 42, value)
}

func TestStateMapPublishesPartialMutationBeforeReturningErrorInternal(t *testing.T) {
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
