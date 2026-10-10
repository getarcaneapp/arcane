package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	environmenttypes "github.com/getarcaneapp/arcane/types/v2/environment"
	eventtypes "github.com/getarcaneapp/arcane/types/v2/event"
	streamtypes "github.com/getarcaneapp/arcane/types/v2/stream"
	versiontypes "github.com/getarcaneapp/arcane/types/v2/version"
	"go.getarcane.app/streams/agg"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/dashboard"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

const (
	clientStreamHeartbeatInterval = 15 * time.Second
	clientStreamDeadPeerTimeout   = 45 * time.Second
	clientStreamEventBuffer       = 64
	// Per-channel handoff buffer; small because the shared writer is the real backpressure point.
	clientStreamChannelBuffer = 16
)

// StreamHandler multiplexes every live feed onto one connection.
type StreamHandler struct {
	events      *event.EventService
	dashboard   *dashboard.DashboardHandler
	activity    *activity.ActivityHandler
	environment *environment.EnvironmentHandler
	version     *version.VersionService
}

type StreamClientInput struct {
	Channels     string `query:"channels" doc:"Comma-separated channels to subscribe to: environments, dashboard, activities, events, version"`
	DebugAllGood bool   `query:"debugAllGood" default:"false" doc:"Debug mode for the dashboard channel: force an empty action item list"`
	Limit        int    `query:"limit" default:"0" doc:"Maximum activities to include in each activities-channel snapshot"`
}

// RegisterStream registers the multiplexed client stream, reusing each domain's producer for its channel.
func RegisterStream(
	api huma.API,
	dashboardHandler *dashboard.DashboardHandler,
	activityHandler *activity.ActivityHandler,
	environmentHandler *environment.EnvironmentHandler,
	eventService *event.EventService,
	versionService *version.VersionService,
) {
	h := &StreamHandler{
		events:      eventService,
		dashboard:   dashboardHandler,
		activity:    activityHandler,
		environment: environmentHandler,
		version:     versionService,
	}

	meter := otel.Meter(tracing.InstrumentationName)
	connections, err := meter.Int64UpDownCounter("arcane.sse.connections",
		metric.WithDescription("Open client SSE stream connections"), metric.WithUnit("{connection}"))
	if err != nil {
		otel.Handle(err)
	}

	sse.Register(api, huma.Operation{
		OperationID: "streamClient",
		Method:      http.MethodGet,
		Path:        "/stream",
		Summary:     "Multiplexed client stream",
		Description: "Streams the requested channels (environments, dashboard, activities, events, version) as JSON envelopes over a single Server-Sent Events connection",
		Tags:        []string{"Stream"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
			httpx.SetStreamHeaders(ctx)
			// The HTTP tracing middleware skips streams, so the lifetime span is a root unless the client sent a traceparent.
			carrier := propagation.MapCarrier{}
			for _, field := range otel.GetTextMapPropagator().Fields() {
				if value := ctx.Header(field); value != "" {
					carrier[field] = value
				}
			}
			parent := otel.GetTextMapPropagator().Extract(ctx.Context(), carrier)
			_, span := otel.Tracer(tracing.InstrumentationName).Start(parent, "sse "+ctx.Operation().Path,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.route", ctx.Operation().Path),
					attribute.String("arcane.sse.channels", ctx.Query("channels")),
					attribute.String("client.address", ctx.RemoteAddr()),
				))
			metricCtx := context.WithoutCancel(ctx.Context())
			connections.Add(metricCtx, 1)
			defer func() {
				connections.Add(metricCtx, -1)
				span.End()
			}()
			next(ctx)
		}},
		// Ungated: each channel checks its own permission; a caller with none gets a heartbeat-only stream.
	}, map[string]any{"message": streamtypes.Event{}}, h.StreamClient)
}

func (h *StreamHandler) StreamClient(ctx context.Context, input *StreamClientInput, send sse.Sender) {
	ps, _ := middleware.PermissionsFromContext(ctx)
	releaseDeadPeerTimeout, timeoutErr := httpx.AcquireDeadPeerTimeout(ctx, clientStreamDeadPeerTimeout)
	if timeoutErr != nil {
		slog.DebugContext(ctx, "could not bound client stream dead-peer timeout", "error", timeoutErr)
	}
	defer releaseDeadPeerTimeout()

	// Select the producers for the requested channels, dropping any the caller lacks permission for.
	requested := make(map[string]bool, 5)
	for name := range strings.SplitSeq(input.Channels, ",") {
		if trimmed := strings.TrimSpace(strings.ToLower(name)); trimmed != "" {
			requested[trimmed] = true
		}
	}
	producers := make([]agg.Producer[streamtypes.Event], 0, 5)

	if requested[streamtypes.ChannelEnvironments] {
		// Ungated like listEnvironments; the producer filters to the caller's accessible environments.
		producers = append(producers, forwardStreamChannel(
			streamtypes.ChannelEnvironments,
			func(channel string, event environmenttypes.StreamEvent) streamtypes.Event {
				return streamtypes.Event{Channel: channel, Environment: &event, Timestamp: event.Timestamp}
			},
			func(producerCtx context.Context, events chan<- environmenttypes.StreamEvent) {
				h.environment.RunStreamProducer(producerCtx, ps, events)
			},
		))
	}

	if requested[streamtypes.ChannelDashboard] {
		wrapDashboard := func(channel string, event dashboardtypes.StreamEvent) streamtypes.Event {
			return streamtypes.Event{Channel: channel, Dashboard: &event, Timestamp: event.Timestamp}
		}
		if ps.Allows(authz.PermDashboardRead, environment.LocalEnvironmentID) {
			producers = append(producers, forwardStreamChannel(
				streamtypes.ChannelDashboard, wrapDashboard,
				func(producerCtx context.Context, events chan<- dashboardtypes.StreamEvent) {
					h.dashboard.RunLocalStreamProducer(producerCtx, input.DebugAllGood, events)
				},
			))
		}
		if ps.AllowsAny(authz.PermDashboardRead) {
			producers = append(producers, forwardStreamChannel(
				streamtypes.ChannelDashboard, wrapDashboard,
				func(producerCtx context.Context, events chan<- dashboardtypes.StreamEvent) {
					h.dashboard.RunRemoteStreamPollers(producerCtx, ps, input.DebugAllGood, events)
				},
			))
		}
	}

	if requested[streamtypes.ChannelActivities] {
		wrapActivity := func(channel string, event activitytypes.StreamEvent) streamtypes.Event {
			return streamtypes.Event{Channel: channel, Activity: &event, Timestamp: event.Timestamp}
		}
		if ps.Allows(authz.PermActivitiesRead, environment.LocalEnvironmentID) {
			producers = append(producers, forwardStreamChannel(
				streamtypes.ChannelActivities, wrapActivity,
				func(producerCtx context.Context, events chan<- activitytypes.StreamEvent) {
					h.activity.RunLocalStreamProducer(producerCtx, input.Limit, events)
				},
			))
		}
		if ps.AllowsAny(authz.PermActivitiesRead) {
			producers = append(producers, forwardStreamChannel(
				streamtypes.ChannelActivities, wrapActivity,
				func(producerCtx context.Context, events chan<- activitytypes.StreamEvent) {
					h.activity.RunRemoteStreamPollers(producerCtx, ps, input.Limit, events)
				},
			))
		}
	}

	if requested[streamtypes.ChannelEvents] && ps.Allows(authz.PermEventsRead, "") {
		producers = append(producers, forwardStreamChannel(
			streamtypes.ChannelEvents,
			func(channel string, event eventtypes.StreamEvent) streamtypes.Event {
				return streamtypes.Event{Channel: channel, EventLog: &event, Timestamp: event.Timestamp}
			},
			h.events.RunStreamProducer,
		))
	}

	if requested[streamtypes.ChannelVersion] {
		// Ungated like GET /app-version.
		producers = append(producers, forwardStreamChannel(
			streamtypes.ChannelVersion,
			func(channel string, event versiontypes.StreamEvent) streamtypes.Event {
				return streamtypes.Event{Channel: channel, Version: &event, Timestamp: event.Timestamp}
			},
			h.version.RunStreamProducer,
		))
	}

	streamCtx, cancel := context.WithCancel(ctx)
	events := make(chan streamtypes.Event, clientStreamEventBuffer)
	var running sync.WaitGroup
	for _, producer := range producers {
		running.Go(func() {
			producer(streamCtx, events)
		})
	}
	defer running.Wait()
	defer cancel()

	heartbeat := time.NewTicker(clientStreamHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		var envelope streamtypes.Event
		select {
		case <-streamCtx.Done():
			return
		case envelope = <-events:
		case <-heartbeat.C:
			envelope = streamtypes.Event{Type: "heartbeat", Timestamp: time.Now()}
		}
		if streamCtx.Err() != nil {
			return
		}
		if err := send.Data(envelope); err != nil {
			slog.DebugContext(ctx, "client stream disconnected", "error", err)
			return
		}
	}
}

// forwardStreamChannel adapts a channel's producer, which keeps emitting its native event type, to the shared envelope.
func forwardStreamChannel[T any](
	channel string,
	wrap func(string, T) streamtypes.Event,
	producer func(context.Context, chan<- T),
) agg.Producer[streamtypes.Event] {
	return func(ctx context.Context, out chan<- streamtypes.Event) {
		inner := make(chan T, clientStreamChannelBuffer)

		var worker sync.WaitGroup
		worker.Go(func() {
			defer close(inner)
			producer(ctx, inner)
		})
		defer worker.Wait()

		for event := range inner {
			if !agg.Send(ctx, out, wrap(channel, event)) {
				return
			}
		}
	}
}
