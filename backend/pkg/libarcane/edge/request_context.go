package edge

import "context"

type internalTunnelRequestContextKey struct{}

// IsInternalTunnelRequest reports whether a request is being dispatched by the
// in-process edge tunnel client instead of a real network listener.
func IsInternalTunnelRequest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	isInternal, _ := ctx.Value(internalTunnelRequestContextKey{}).(bool)
	return isInternal
}

func withInternalTunnelRequestInternal(ctx context.Context) context.Context {
	return context.WithValue(ctx, internalTunnelRequestContextKey{}, true)
}
