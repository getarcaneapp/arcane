package utils

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type activityContextTestKey string

func TestActivityRuntimeContextPrefersAppContext(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(t.Context())
	appCtx, cancelApp := context.WithCancel(t.Context())
	defer cancelApp()

	ctx := ActivityRuntimeContext(requestCtx, appCtx)

	cancelRequest()
	require.NoError(t, ctx.Err())

	cancelApp()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestActivityRuntimeContextPreservesRequestValuesWithAppCancellation(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.WithValue(t.Context(), activityContextTestKey("request-id"), "req-123"))
	appCtx, cancelApp := context.WithCancel(WithAppLifecycleContext(t.Context()))
	defer cancelApp()

	ctx := ActivityRuntimeContext(requestCtx, appCtx)

	require.Equal(t, "req-123", ctx.Value(activityContextTestKey("request-id")))
	require.True(t, IsAppLifecycleContext(ctx))

	cancelRequest()
	require.NoError(t, ctx.Err())

	cancelApp()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestActivityRuntimeContextUsesAppDeadlineWithRequestValues(t *testing.T) {
	requestDeadline := time.Now().Add(time.Hour)
	appDeadline := time.Now().Add(time.Minute)
	requestCtx, cancelRequest := context.WithDeadline(context.WithValue(t.Context(), activityContextTestKey("trace-id"), "trace-123"), requestDeadline)
	defer cancelRequest()
	appCtx, cancelApp := context.WithDeadline(t.Context(), appDeadline)
	defer cancelApp()

	ctx := ActivityRuntimeContext(requestCtx, appCtx)

	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.Equal(t, appDeadline, deadline)
	require.Equal(t, "trace-123", ctx.Value(activityContextTestKey("trace-id")))
}

func TestActivityRuntimeContextFallsBackToDetachedRequestContext(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(t.Context())

	ctx := ActivityRuntimeContext(requestCtx, nil)

	cancelRequest()
	require.NoError(t, ctx.Err())
}

func TestActivityRuntimeContextPreservesMarkedAppContext(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(WithAppLifecycleContext(t.Context()))

	ctx := ActivityRuntimeContext(appCtx, nil)

	cancelApp()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}
