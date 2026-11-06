package ws

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/container"
	wshub "getarcane.dev/arcane/v3/pkg/ws"
	"getarcane.dev/types/v3"
)

type (
	containersStatsResult struct {
		samples map[string]*types.ContainerResourceSample
		err     error
	}
)

const (
	containersStatsInterval  = 2 * time.Second
	containersStatsReadLimit = 64 << 10
)

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
