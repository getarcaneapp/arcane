package httpx

import (
	"context"
	"log/slog"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"go.getarcane.app/streams/agg"
)

// streamDeadPeerTimeout is how long an aggregate stream's writes may stay
// unacknowledged before the connection is torn down. A client that has
// acknowledged nothing for this long is gone: healthy ones acknowledge the
// heartbeat every 15 seconds.
const streamDeadPeerTimeout = 45 * time.Second

// RunAuthorizedAggregateStream selects local and remote producers from the
// caller's effective permissions before starting an aggregate stream. Remote
// producers must still filter individual environments with PermissionSet.Allows.
func RunAuthorizedAggregateStream[T any](
	ctx context.Context,
	ps *authz.PermissionSet,
	permission string,
	config agg.Config[T],
	localProducer agg.Producer[T],
	remoteProducer agg.Producer[T],
) error {
	config.Producers = make([]agg.Producer[T], 0, 2)
	if ps.Allows(permission, "0") {
		config.Producers = append(config.Producers, localProducer)
	}
	if ps.AllowsAny(permission) {
		config.Producers = append(config.Producers, remoteProducer)
	}

	releaseDeadPeerTimeout, timeoutErr := AcquireDeadPeerTimeout(ctx, streamDeadPeerTimeout)
	if timeoutErr != nil {
		slog.DebugContext(ctx, "could not bound stream dead-peer timeout", "permission", permission, "error", timeoutErr)
	}
	defer releaseDeadPeerTimeout()

	startedAt := time.Now()
	err := agg.Run(ctx, config)

	// Once the response headers are sent there is no way to report a failure to
	// the client, and the access log only records the connection's total
	// lifetime — so a client that vanishes mid-write is invisible without this.
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.WarnContext(ctx, "aggregate stream ended with error", "permission", permission, "error", err, "duration", time.Since(startedAt))
		return err
	}
	slog.DebugContext(ctx, "aggregate stream closed", "permission", permission, "duration", time.Since(startedAt), "context_error", ctx.Err())
	return err
}
