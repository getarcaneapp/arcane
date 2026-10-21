package ws

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
)

const (
	// containersStatsInterval is the wall-clock period every subscription samples on.
	containersStatsInterval = 2 * time.Second
	// containersStatsReadLimit fits a subscription of 500 full container IDs, the most one batch samples.
	containersStatsReadLimit = 64 << 10
)

type containersStatsResult struct {
	samples map[string]*types.ContainerResourceSample
	err     error
}

// ContainersStats streams resource samples for a client-chosen set of containers over one WebSocket.
// The client sends a ResourceSampleSubscription whenever its set changes; the server answers with a
// ResourceSampleBatch every couple of seconds.
//
//	@Summary		Get batched container stats via WebSocket
//	@Description	Stream resource samples for the subscribed containers over one WebSocket connection
//	@Tags			WebSocket
//	@Param			id	path	string	true	"Environment ID"
//	@Router			/api/environments/{id}/ws/containers/stats [get]
func (h *WebSocketHandler) ContainersStats(c *echo.Context) error {
	conn, unregister, ok := h.acceptWS(c, types.WSKindContainerStats, "batch")
	if !ok {
		return nil
	}
	defer unregister()
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(containersStatsReadLimit)

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	var ids atomic.Pointer[[]string]
	subscribed := make(chan struct{}, 1)
	go readContainersStatsPump(ctx, cancel, conn, &ids, subscribed)
	// The pong is serviced by readContainersStatsPump.
	go wshub.KeepAlive(ctx, cancel, conn)

	send := func(batch types.ContainerResourceSampleBatch) error {
		b, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return conn.Write(wctx, websocket.MessageText, b)
	}

	// Nothing to sample until the first subscription arrives.
	select {
	case <-ctx.Done():
		return nil
	case <-subscribed:
	}

	for ctx.Err() == nil {
		want := kit.FromPtr(ids.Load())
		want = want[:min(len(want), container.ResourceSampleLimit)]
		subCtx, cancelSub := context.WithCancel(ctx)
		go func() {
			defer cancelSub()
			select {
			case <-subscribed:
			case <-subCtx.Done():
			}
		}()
		h.containersStatsHub.Subscribe(subCtx, strings.Join(want, ","),
			func(runCtx context.Context, publish func(containersStatsResult)) {
				for {
					samples, err := h.containerService.SampleResources(runCtx, want, containersStatsInterval/4)
					if runCtx.Err() != nil {
						return
					}
					if err != nil {
						slog.DebugContext(runCtx, "Failed to sample container stats", "envId", c.Param("id"), "error", err)
					}
					publish(containersStatsResult{samples: samples, err: err})
					select {
					case <-runCtx.Done():
						return
					case <-time.After(time.Until(time.Now().Truncate(containersStatsInterval).Add(containersStatsInterval))):
					}
				}
			},
			func(result containersStatsResult) bool {
				if result.err != nil {
					return send(types.ContainerResourceSampleBatch{Error: "Failed to sample container stats: " + result.err.Error(), Timestamp: time.Now()}) == nil
				}
				return send(types.ContainerResourceSampleBatch{Samples: result.samples, Timestamp: time.Now()}) == nil
			})
		if subCtx.Err() == nil {
			return nil
		}
	}
	return nil
}

// readContainersStatsPump is the single reader for the ContainersStats websocket. It stores each
// subscription and wakes the sampler; malformed frames are ignored. Do not add other readers.
func readContainersStatsPump(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, ids *atomic.Pointer[[]string], subscribed chan<- struct{}) {
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var subscription types.ContainerResourceSampleSubscription
		if json.Unmarshal(data, &subscription) != nil {
			continue
		}
		ids.Store(new(subscription.ContainerIDs))
		select {
		case subscribed <- struct{}{}:
		default:
		}
	}
}
