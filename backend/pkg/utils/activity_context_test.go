package utils

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type activityContextTestKey string

func TestActivityRuntimeContextPrefersAppContextInternal(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	ctx := ActivityRuntimeContext(requestCtx, appCtx)

	cancelRequest()
	require.NoError(t, ctx.Err())

	cancelApp()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestActivityRuntimeContextPreservesRequestValuesWithAppCancellationInternal(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.WithValue(context.Background(), activityContextTestKey("request-id"), "req-123"))
	appCtx, cancelApp := context.WithCancel(WithAppLifecycleContext(context.Background()))
	defer cancelApp()

	ctx := ActivityRuntimeContext(requestCtx, appCtx)

	require.Equal(t, "req-123", ctx.Value(activityContextTestKey("request-id")))
	require.True(t, IsAppLifecycleContext(ctx))

	cancelRequest()
	require.NoError(t, ctx.Err())

	cancelApp()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestActivityRuntimeContextUsesAppDeadlineWithRequestValuesInternal(t *testing.T) {
	requestDeadline := time.Now().Add(time.Hour)
	appDeadline := time.Now().Add(time.Minute)
	requestCtx, cancelRequest := context.WithDeadline(context.WithValue(context.Background(), activityContextTestKey("trace-id"), "trace-123"), requestDeadline)
	defer cancelRequest()
	appCtx, cancelApp := context.WithDeadline(context.Background(), appDeadline)
	defer cancelApp()

	ctx := ActivityRuntimeContext(requestCtx, appCtx)

	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.Equal(t, appDeadline, deadline)
	require.Equal(t, "trace-123", ctx.Value(activityContextTestKey("trace-id")))
}

func TestActivityRuntimeContextFallsBackToDetachedRequestContextInternal(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())

	ctx := ActivityRuntimeContext(requestCtx, nil)

	cancelRequest()
	require.NoError(t, ctx.Err())
}

func TestActivityRuntimeContextPreservesMarkedAppContextInternal(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(WithAppLifecycleContext(context.Background()))

	ctx := ActivityRuntimeContext(appCtx, nil)

	cancelApp()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestWithAppLifecycleContextMarksContextInternal(t *testing.T) {
	ctx := WithAppLifecycleContext(context.Background())

	require.True(t, IsAppLifecycleContext(ctx))
}
