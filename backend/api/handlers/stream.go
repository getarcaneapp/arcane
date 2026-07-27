package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	httputils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	environmenttypes "github.com/getarcaneapp/arcane/types/v2/environment"
	streamtypes "github.com/getarcaneapp/arcane/types/v2/stream"
	"go.getarcane.app/streams/agg"
)

const (
	clientStreamHeartbeatInterval = 15 * time.Second
	clientStreamDeadPeerTimeout   = 45 * time.Second
	clientStreamEventBuffer       = 64
	// Per-channel handoff buffer between a channel's producer and the envelope
	// forwarder. Small on purpose: the shared writer is the real backpressure
	// point, and a deep per-channel buffer would only delay noticing a stalled
	// client.
	clientStreamChannelBuffer = 16
)

// StreamHandler multiplexes every live feed onto one connection.
type StreamHandler struct {
	dashboard   *DashboardHandler
	activity    *ActivityHandler
	environment *EnvironmentHandler
}

type StreamClientInput struct {
	Channels     string `query:"channels" doc:"Comma-separated channels to subscribe to: environments, dashboard, activities"`
	DebugAllGood bool   `query:"debugAllGood" default:"false" doc:"Debug mode for the dashboard channel: force an empty action item list"`
	Limit        int    `query:"limit" default:"0" doc:"Maximum activities to include in each activities-channel snapshot"`
}

// RegisterStream registers the multiplexed client stream. It builds its own
// per-domain handlers from the same services the individual handlers use, so
// each channel's producer stays the single implementation of that feed.
func RegisterStream(
	api huma.API,
	dashboardService *services.DashboardService,
	activityService *services.ActivityService,
	environmentService *services.EnvironmentService,
) {
	h := &StreamHandler{
		dashboard: &DashboardHandler{
			dashboardService:   dashboardService,
			environmentService: environmentService,
		},
		activity: &ActivityHandler{
			activityService:    activityService,
			environmentService: environmentService,
		},
		// Only the environment service is needed: the channel producer lists
		// environments and derives liveness, nothing more.
		environment: &EnvironmentHandler{environmentService: environmentService},
	}

	huma.Register(api, huma.Operation{
		OperationID: "streamClient",
		Method:      http.MethodGet,
		Path:        "/stream",
		Summary:     "Multiplexed client stream",
		Description: "Streams the requested channels (environments, dashboard, activities) over a single JSON-lines connection",
		Tags:        []string{"Stream"},
		Security:    defaultOperationSecurityInternal(),
		// Ungated: each channel applies its own permission check below, and a
		// caller with access to none simply gets a heartbeat-only stream.
	}, h.StreamClient)
}

func (h *StreamHandler) StreamClient(ctx context.Context, input *StreamClientInput) (*huma.StreamResponse, error) {
	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // streaming work must use humaCtx.Context()
			httputils.SetJSONStreamHeaders(humaCtx)

			writer := humaCtx.BodyWriter()
			flush := func() {
				if f, ok := writer.(http.Flusher); ok {
					f.Flush()
				}
			}

			ps, _ := humamw.PermissionsFromContext(humaCtx.Context())
			h.streamClientInternal(humaCtx.Context(), ps, input, writer, flush)
		},
	}, nil
}

func (h *StreamHandler) streamClientInternal(ctx context.Context, ps *authz.PermissionSet, input *StreamClientInput, writer io.Writer, flush func()) {
	releaseDeadPeerTimeout, timeoutErr := httputils.AcquireDeadPeerTimeout(ctx, clientStreamDeadPeerTimeout)
	if timeoutErr != nil {
		slog.DebugContext(ctx, "could not bound client stream dead-peer timeout", "error", timeoutErr)
	}
	defer releaseDeadPeerTimeout()

	// Nothing can be reported to a client whose headers were sent long ago.
	_ = agg.Run(ctx, agg.Config[streamtypes.Event]{
		Writer:            writer,
		Flush:             flush,
		Buffer:            clientStreamEventBuffer,
		HeartbeatInterval: clientStreamHeartbeatInterval,
		MakeHeartbeat: func() streamtypes.Event {
			return streamtypes.Event{Type: "heartbeat", Timestamp: time.Now()}
		},
		Producers: h.producersForInternal(ps, input),
	})
}

// producersForInternal selects the producers for the requested channels,
// dropping any the caller lacks permission for.
func (h *StreamHandler) producersForInternal(ps *authz.PermissionSet, input *StreamClientInput) []agg.Producer[streamtypes.Event] {
	requested := parseStreamChannelsInternal(input.Channels)
	producers := make([]agg.Producer[streamtypes.Event], 0, 4)

	if requested[streamtypes.ChannelEnvironments] {
		// Ungated like listEnvironments; the producer filters to the caller's
		// accessible environments.
		producers = append(producers, forwardStreamChannelInternal(
			streamtypes.ChannelEnvironments,
			func(channel string, event environmenttypes.StreamEvent) streamtypes.Event {
				return streamtypes.Event{Channel: channel, Environment: &event, Timestamp: event.Timestamp}
			},
			func(producerCtx context.Context, events chan<- environmenttypes.StreamEvent) {
				h.environment.runEnvironmentStreamProducerInternal(producerCtx, ps, events)
			},
		))
	}

	if requested[streamtypes.ChannelDashboard] {
		wrapDashboard := func(channel string, event dashboardtypes.StreamEvent) streamtypes.Event {
			return streamtypes.Event{Channel: channel, Dashboard: &event, Timestamp: event.Timestamp}
		}
		if ps.Allows(authz.PermDashboardRead, localDockerEnvironmentID) {
			producers = append(producers, forwardStreamChannelInternal(
				streamtypes.ChannelDashboard, wrapDashboard,
				func(producerCtx context.Context, events chan<- dashboardtypes.StreamEvent) {
					h.dashboard.runLocalDashboardStreamProducerInternal(producerCtx, input.DebugAllGood, events)
				},
			))
		}
		if ps.AllowsAny(authz.PermDashboardRead) {
			producers = append(producers, forwardStreamChannelInternal(
				streamtypes.ChannelDashboard, wrapDashboard,
				func(producerCtx context.Context, events chan<- dashboardtypes.StreamEvent) {
					h.dashboard.runRemoteDashboardStreamPollersInternal(producerCtx, ps, input.DebugAllGood, events)
				},
			))
		}
	}

	if requested[streamtypes.ChannelActivities] {
		wrapActivity := func(channel string, event activitytypes.StreamEvent) streamtypes.Event {
			return streamtypes.Event{Channel: channel, Activity: &event, Timestamp: event.Timestamp}
		}
		if ps.Allows(authz.PermActivitiesRead, localDockerEnvironmentID) {
			producers = append(producers, forwardStreamChannelInternal(
				streamtypes.ChannelActivities, wrapActivity,
				func(producerCtx context.Context, events chan<- activitytypes.StreamEvent) {
					h.activity.runLocalActivityStreamProducerInternal(producerCtx, input.Limit, events)
				},
			))
		}
		if ps.AllowsAny(authz.PermActivitiesRead) {
			producers = append(producers, forwardStreamChannelInternal(
				streamtypes.ChannelActivities, wrapActivity,
				func(producerCtx context.Context, events chan<- activitytypes.StreamEvent) {
					h.activity.runRemoteActivityStreamPollersInternal(producerCtx, ps, input.Limit, events)
				},
			))
		}
	}

	return producers
}

func parseStreamChannelsInternal(raw string) map[string]bool {
	channels := make(map[string]bool, 3)
	for name := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(strings.ToLower(name)); trimmed != "" {
			channels[trimmed] = true
		}
	}
	return channels
}

// forwardStreamChannelInternal adapts a channel's own producer to the shared
// envelope. Each producer keeps emitting its native event type, so adding a
// channel never means rewriting its feed.
func forwardStreamChannelInternal[T any](
	channel string,
	wrap func(string, T) streamtypes.Event,
	producer func(context.Context, chan<- T),
) agg.Producer[streamtypes.Event] {
	return func(ctx context.Context, out chan<- streamtypes.Event) {
		inner := make(chan T, clientStreamChannelBuffer)

		go func() {
			defer close(inner)
			producer(ctx, inner)
		}()

		for event := range inner {
			if !agg.Send(ctx, out, wrap(channel, event)) {
				return
			}
		}
	}
}
