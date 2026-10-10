package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sync"

	"go.getarcane.app/kit/pkg"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/telemetry"
)

// telemetryProviders owns the providers enabled through the standard OTEL_*
// exporter variables. A nil provider means that signal is off.
type telemetryProviders struct {
	tracerProvider *trace.TracerProvider
	meterProvider  *metric.MeterProvider
	loggerProvider *log.LoggerProvider

	shutdownOnce sync.Once
	shutdownErr  error
}

// setupTelemetry installs global providers for each OTEL_*_EXPORTER that is set
// to something other than none. Exporter failures are reported to errorLogger.
func setupTelemetry(ctx context.Context, cfg *config.Config, errorLogger *slog.Logger, instanceID string) (*telemetryProviders, error) {
	tel := &telemetryProviders{}
	if telemetry.SDKDisabled() {
		return tel, nil
	}
	fail := func(err error) (*telemetryProviders, error) {
		return nil, errors.Join(err, tel.Shutdown(context.WithoutCancel(ctx)))
	}

	mode := "manager"
	if cfg.AgentMode {
		mode = kit.Ternary(cfg.EdgeAgent, "edge", "agent")
	}
	// OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES may override the defaults, but never the
	// persisted instance ID or mode that identify this installation.
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName("arcane"), semconv.ServiceVersion(config.Version)),
		resource.WithFromEnv(),
		resource.WithAttributes(semconv.ServiceInstanceID(instanceID), attribute.String("arcane.mode", mode)),
	)
	if err != nil {
		return fail(fmt.Errorf("create telemetry resource: %w", err))
	}

	// The fallbacks return nil so an unset OTEL_*_EXPORTER means off, not otlp.
	spanExporter, err := autoexport.NewSpanExporter(ctx, autoexport.WithFallbackSpanExporter(func(context.Context) (trace.SpanExporter, error) {
		return nil, nil //nolint:nilnil // nil means tracing is off.
	}))
	if err != nil {
		return fail(fmt.Errorf("create trace exporter: %w", err))
	}
	if spanExporter != nil && !autoexport.IsNoneSpanExporter(spanExporter) {
		tel.tracerProvider = trace.NewTracerProvider(trace.WithResource(res), trace.WithBatcher(redactingExporter{spanExporter}))
	}

	// Adds go.schedule.duration, which runtime.Start does not report; OTEL_METRICS_PRODUCERS overrides it.
	autoexport.WithFallbackMetricProducer(func(context.Context) (metric.Producer, error) { return runtime.NewProducer(), nil })
	metricReader, err := autoexport.NewMetricReader(ctx, autoexport.WithFallbackMetricReader(func(context.Context) (metric.Reader, error) {
		return nil, nil //nolint:nilnil // nil means metrics are off.
	}))
	if err != nil {
		return fail(fmt.Errorf("create metric reader: %w", err))
	}
	if metricReader != nil && !autoexport.IsNoneMetricReader(metricReader) {
		tel.meterProvider = metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(metricReader))
		if err = runtime.Start(runtime.WithMeterProvider(tel.meterProvider)); err != nil {
			return fail(fmt.Errorf("start runtime metrics: %w", err))
		}
	}

	logExporter, err := autoexport.NewLogExporter(ctx, autoexport.WithFallbackLogExporter(func(context.Context) (log.Exporter, error) {
		return nil, nil //nolint:nilnil // nil means log export is off.
	}))
	if err != nil {
		return fail(fmt.Errorf("create log exporter: %w", err))
	}
	if logExporter != nil && !autoexport.IsNoneLogExporter(logExporter) {
		tel.loggerProvider = log.NewLoggerProvider(log.WithResource(res), log.WithProcessor(log.NewBatchProcessor(logExporter)))
	}

	if tel.tracerProvider == nil && tel.meterProvider == nil && tel.loggerProvider == nil {
		return tel, nil
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		errorLogger.WarnContext(ctx, "OpenTelemetry export failed", "error", err)
	}))
	if tel.tracerProvider != nil {
		otel.SetTracerProvider(tel.tracerProvider)
		otel.SetTextMapPropagator(propagation.TraceContext{})
	}
	if tel.meterProvider != nil {
		otel.SetMeterProvider(tel.meterProvider)
	}
	if tel.loggerProvider != nil {
		global.SetLoggerProvider(tel.loggerProvider)
	}
	return tel, nil
}

// Shutdown flushes and stops every provider. It is safe to call more than once.
func (t *telemetryProviders) Shutdown(ctx context.Context) error {
	t.shutdownOnce.Do(func() {
		var errs []error
		if t.tracerProvider != nil {
			errs = append(errs, t.tracerProvider.Shutdown(ctx))
		}
		if t.meterProvider != nil {
			errs = append(errs, t.meterProvider.Shutdown(ctx))
		}
		if t.loggerProvider != nil {
			errs = append(errs, t.loggerProvider.Shutdown(ctx))
		}
		t.shutdownErr = errors.Join(errs...)
	})
	return t.shutdownErr
}

var (
	urlUserinfo  = regexp.MustCompile(`(https?://)[^@/\s"']*@`)
	urlQuery     = regexp.MustCompile(`(https?://[^\s?#"']*)[?#][^\s"']*`)
	webhookToken = regexp.MustCompile(`(/webhooks/trigger/)[^/?#\s"']+`)
)

// redactSecrets strips URL credentials, queries, and fragments, and masks webhook trigger tokens, which travel in the path.
func redactSecrets(s string) string {
	s = urlUserinfo.ReplaceAllString(s, "$1")
	return webhookToken.ReplaceAllString(urlQuery.ReplaceAllString(s, "$1"), "${1}:token")
}

// redactingExporter strips URL queries and fragments from span attributes, events, and statuses before export.
type redactingExporter struct{ trace.SpanExporter }

func (e redactingExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	redacted := make([]trace.ReadOnlySpan, len(spans))
	for i, span := range spans {
		redacted[i] = redactedSpan{span}
	}
	return e.SpanExporter.ExportSpans(ctx, redacted)
}

type redactedSpan struct{ trace.ReadOnlySpan }

func (s redactedSpan) Attributes() []attribute.KeyValue {
	return redactAttributes(s.ReadOnlySpan.Attributes())
}

func (s redactedSpan) Events() []trace.Event {
	events := slices.Clone(s.ReadOnlySpan.Events())
	for i := range events {
		events[i].Attributes = redactAttributes(events[i].Attributes)
	}
	return events
}

func (s redactedSpan) Status() trace.Status {
	status := s.ReadOnlySpan.Status()
	status.Description = redactSecrets(status.Description)
	return status
}

func redactAttributes(attrs []attribute.KeyValue) []attribute.KeyValue {
	redacted := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		switch {
		case kv.Key == "url.query":
			continue
		case kv.Value.Type() == attribute.STRING:
			kv.Value = attribute.StringValue(redactSecrets(kv.Value.AsString()))
		}
		redacted = append(redacted, kv)
	}
	return redacted
}
