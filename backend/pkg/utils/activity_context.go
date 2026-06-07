package utils

import (
	"context"
	"time"
)

type appLifecycleContextKey struct{}

type activityRuntimeContext struct {
	valueCtx     context.Context
	lifecycleCtx context.Context
}

// WithAppLifecycleContext marks ctx as the application lifecycle context.
func WithAppLifecycleContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, appLifecycleContextKey{}, true)
}

// IsAppLifecycleContext reports whether ctx is tied to the application lifecycle.
func IsAppLifecycleContext(ctx context.Context) bool {
	isLifecycle, _ := ctx.Value(appLifecycleContextKey{}).(bool)
	return isLifecycle
}

// ActivityRuntimeContext returns a context suitable for activity-backed work.
func ActivityRuntimeContext(requestCtx context.Context, appCtx context.Context) context.Context {
	if appCtx != nil {
		if requestCtx == nil || requestCtx == appCtx {
			return appCtx
		}
		return activityRuntimeContext{
			valueCtx:     requestCtx,
			lifecycleCtx: appCtx,
		}
	}
	if IsAppLifecycleContext(requestCtx) {
		return requestCtx
	}
	if requestCtx != nil {
		return context.WithoutCancel(requestCtx)
	}
	return context.Background()
}

func (c activityRuntimeContext) Deadline() (time.Time, bool) {
	return c.lifecycleCtx.Deadline()
}

func (c activityRuntimeContext) Done() <-chan struct{} {
	return c.lifecycleCtx.Done()
}

func (c activityRuntimeContext) Err() error {
	return c.lifecycleCtx.Err()
}

func (c activityRuntimeContext) Value(key any) any {
	if _, ok := key.(appLifecycleContextKey); ok {
		if value := c.lifecycleCtx.Value(key); value != nil {
			return value
		}
	}
	if c.valueCtx != nil {
		if value := c.valueCtx.Value(key); value != nil {
			return value
		}
	}
	return c.lifecycleCtx.Value(key)
}
