package actors

import (
	"context"
	"maps"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

type stateTestValueInternal struct {
	values map[string]int
}

func TestStateSerializesMutationsAndPublishesClonedSnapshotsInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	state, err := NewState(
		t.Context(),
		runtime,
		"state-test",
		"values",
		1,
		stateTestValueInternal{values: make(map[string]int)},
		func(value stateTestValueInternal) stateTestValueInternal {
			value.values = maps.Clone(value.values)
			return value
		},
	)
	require.NoError(t, err)

	expectedErr := errors.New("mutation failed")
	err = state.Apply(t.Context(), "mutate state", func(_ context.Context, value *stateTestValueInternal) error {
		value.values["answer"] = 42
		return expectedErr
	})
	require.ErrorIs(t, err, expectedErr)
	snapshot, ok := state.Load()
	require.True(t, ok)
	require.Equal(t, 42, snapshot.values["answer"])

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, state.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}
