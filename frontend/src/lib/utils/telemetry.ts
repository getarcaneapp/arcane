import type { NavigationType } from '$app/navigation';
import type { Context, Counter, Histogram, Span } from '@opentelemetry/api';
import type { LogRecord } from '@opentelemetry/api-logs';
import type { Resource } from '@opentelemetry/resources';
import type { Metric } from 'web-vitals';

type Signal = 'traces' | 'metrics' | 'logs';

const SERVICE_NAME = 'arcane-frontend';
// Keeps each export well under the relay's 512 KiB request limit.
const MAX_EXPORT_BATCH_SIZE = 32;
// Aborted fetches and logout cancellations are expected, not failures.
const CANCELLED_ERRORS = ['AbortError', 'SessionCancelledError'];

// Query strings and fragments can carry OIDC codes and tokens, so they never leave the browser.
function stripUrlQuery(value: string): string {
	return value.replace(/(https?:\/\/[^\s?#"']*)[?#][^\s"']*/g, '$1').replace(/^(\/[^\s?#]*)[?#].*$/, '$1');
}

let otel: typeof import('@opentelemetry/api') | undefined;
let resource: Promise<Resource> | undefined;
const initializations: Partial<Record<Signal, Promise<void>>> = {};
const providers: { forceFlush(): Promise<void> }[] = [];
let route = '/';
let navigationStart = 0;
let navigated = false;
let pageSpan: Span | undefined;
let pageContext: Context | undefined;
let navigationMetrics: { count: Counter; duration: Histogram } | undefined;
let emitErrorLog: ((record: LogRecord) => void) | undefined;

const initializers: Record<Signal, (resource: Resource) => Promise<void>> = {
	traces: async (resource) => {
		const [api, sdk, exporter, instrumentation, documentLoad, fetch] = await Promise.all([
			import('@opentelemetry/api'),
			import('@opentelemetry/sdk-trace-web'),
			import('@opentelemetry/exporter-trace-otlp-http'),
			import('@opentelemetry/instrumentation'),
			import('@opentelemetry/instrumentation-document-load'),
			import('@opentelemetry/instrumentation-fetch')
		]);
		// Spans started outside any active span (fetches, logs) belong to the current pageview.
		class PageContextManager extends sdk.StackContextManager {
			override active(): Context {
				const active = super.active();
				return pageContext && !api.trace.getSpan(active) ? pageContext : active;
			}
		}
		const provider = new sdk.WebTracerProvider({
			resource,
			spanProcessors: [
				{
					onStart: () => {},
					onEnd: (span) => {
						for (const attributes of [span.attributes, ...span.events.map((event) => event.attributes ?? {})]) {
							for (const [key, value] of Object.entries(attributes)) {
								if (key === 'url.query') delete attributes[key];
								else if (typeof value === 'string') attributes[key] = stripUrlQuery(value);
							}
						}
					},
					forceFlush: () => Promise.resolve(),
					shutdown: () => Promise.resolve()
				},
				new sdk.BatchSpanProcessor(new exporter.OTLPTraceExporter({ url: '/api/telemetry/traces' }), {
					maxExportBatchSize: MAX_EXPORT_BATCH_SIZE
				})
			]
		});
		provider.register({ contextManager: new PageContextManager() });
		providers.push(provider);
		instrumentation.registerInstrumentations({
			tracerProvider: provider,
			instrumentations: [
				new documentLoad.DocumentLoadInstrumentation(),
				new fetch.FetchInstrumentation({
					// The SSE stream never ends, and tracing it would keep the connection open after the app cancels it.
					ignoreUrls: [/\/api\/telemetry\//, /\/api\/stream(\?|$)/],
					applyCustomAttributesOnSpan: (span, _request, result) => {
						if (result instanceof Response || (result instanceof Error && CANCELLED_ERRORS.includes(result.name))) return;
						span.setAttribute('error.type', result instanceof Error ? result.name : 'Error');
						span.setStatus({ code: api.SpanStatusCode.ERROR, message: stripUrlQuery(result.message) });
					}
				})
			]
		});
		otel = api;
		// Tracing can finish loading after the first navigation; start the pageview it missed.
		if (navigated) startPageView(route, navigationStart);
	},
	metrics: async (resource) => {
		const [sdk, exporter, vitals] = await Promise.all([
			import('@opentelemetry/sdk-metrics'),
			import('@opentelemetry/exporter-metrics-otlp-http'),
			import('web-vitals')
		]);
		const provider = new sdk.MeterProvider({
			resource,
			readers: [
				new sdk.PeriodicExportingMetricReader({
					exporter: new exporter.OTLPMetricExporter({ url: '/api/telemetry/metrics' }),
					exportIntervalMillis: 60_000
				})
			]
		});
		providers.push(provider);
		const meter = provider.getMeter(SERVICE_NAME);
		navigationMetrics = {
			count: meter.createCounter('browser.navigation.count', { description: 'Client-side navigations' }),
			duration: meter.createHistogram('browser.navigation.duration', {
				unit: 'ms',
				description: 'Time until a navigation completes'
			})
		};
		const webVitals: Record<Metric['name'], Histogram> = {
			CLS: meter.createHistogram('browser.web_vital.cls', {
				advice: { explicitBucketBoundaries: [0, 0.01, 0.05, 0.1, 0.15, 0.25, 0.5, 1] }
			}),
			FCP: meter.createHistogram('browser.web_vital.fcp', { unit: 'ms' }),
			INP: meter.createHistogram('browser.web_vital.inp', { unit: 'ms' }),
			LCP: meter.createHistogram('browser.web_vital.lcp', { unit: 'ms' }),
			TTFB: meter.createHistogram('browser.web_vital.ttfb', { unit: 'ms' })
		};
		const report = (metric: Metric) =>
			webVitals[metric.name].record(metric.value, { 'http.route': route, 'web_vital.rating': metric.rating });
		vitals.onCLS(report);
		vitals.onFCP(report);
		vitals.onINP(report);
		vitals.onLCP(report);
		vitals.onTTFB(report);
	},
	logs: async (resource) => {
		const [api, sdk, exporter] = await Promise.all([
			import('@opentelemetry/api-logs'),
			import('@opentelemetry/sdk-logs'),
			import('@opentelemetry/exporter-logs-otlp-http')
		]);
		const provider = new sdk.LoggerProvider({
			resource,
			processors: [
				new sdk.BatchLogRecordProcessor({
					exporter: new exporter.OTLPLogExporter({ url: '/api/telemetry/logs' }),
					maxExportBatchSize: MAX_EXPORT_BATCH_SIZE
				})
			]
		});
		providers.push(provider);
		const logger = provider.getLogger(SERVICE_NAME);
		emitErrorLog = (record) => logger.emit({ ...record, severityNumber: api.SeverityNumber.ERROR, severityText: 'ERROR' });
		window.addEventListener('error', (event) => recordError(event.error ?? event.message, 'window.error'));
		window.addEventListener('unhandledrejection', (event) => recordError(event.reason, 'window.unhandledrejection'));
	}
};

// Lazily loads the SDK for each enabled signal once; nothing loads while every signal is off.
export function setTelemetry(enabled: Record<Signal, boolean>, version: string): Promise<void> {
	if (typeof window === 'undefined') return Promise.resolve();
	const signals = (Object.keys(initializers) as Signal[]).filter((signal) => enabled[signal]);
	if (signals.length === 0) return Promise.resolve();

	const loaded = (resource ??= Promise.all([
		import('@opentelemetry/resources'),
		import('@opentelemetry/semantic-conventions'),
		import('@opentelemetry/opentelemetry-browser-detector'),
		import('#lib/utils/streaming.js')
	]).then(([resources, semconv, detector, streaming]) => {
		const flush = (event: Event) => {
			if (event.type === 'pagehide') pageSpan?.end();
			else if (document.visibilityState !== 'hidden') return;
			for (const provider of providers) void provider.forceFlush().catch(() => {});
		};
		window.addEventListener('visibilitychange', flush);
		window.addEventListener('pagehide', flush);
		return resources
			.resourceFromAttributes({
				[semconv.ATTR_SERVICE_NAME]: SERVICE_NAME,
				[semconv.ATTR_SERVICE_VERSION]: version,
				// Each page session owns its cumulative metric series, so tabs never overwrite each other.
				[semconv.ATTR_SERVICE_INSTANCE_ID]: streaming.streamCacheBuster()
			})
			.merge(resources.detectResources({ detectors: [detector.browserDetector] }));
	}));
	// A failed chunk download is forgotten so the next navigation retries instead of staying off.
	loaded.catch(() => {
		if (resource === loaded) resource = undefined;
	});
	return Promise.all(
		signals.map(
			(signal) =>
				(initializations[signal] ??= loaded.then(initializers[signal]).catch(() => {
					delete initializations[signal];
				}))
		)
	).then(() => {});
}

// Starts the pageview span that parents this navigation's requests.
export function startPageView(routeId: string | null | undefined, startTime = performance.now()): void {
	route = (routeId ?? '').replace(/\/\([^)]*\)/g, '') || '/';
	navigationStart = startTime;
	navigated = true;
	if (!otel) return;
	pageSpan?.end();
	pageSpan = otel.trace
		.getTracer(SERVICE_NAME)
		.startSpan(`pageview ${route}`, { root: true, startTime, attributes: { 'http.route': route } });
	pageContext = otel.trace.setSpan(otel.ROOT_CONTEXT, pageSpan);
}

// The initial 'enter' navigation starts its pageview here and is measured from the document's time origin.
export function finishNavigation(routeId: string | null | undefined, type: NavigationType): void {
	if (type === 'enter') startPageView(routeId, 0);
	const attributes = { 'http.route': route, 'navigation.type': type };
	navigationMetrics?.count.add(1, attributes);
	navigationMetrics?.duration.record(performance.now() - navigationStart, attributes);
}

// Emits a sanitized error log with the current route and trace context.
export function recordError(error: unknown, source: string): void {
	if (!emitErrorLog || (error instanceof Error && CANCELLED_ERRORS.includes(error.name))) return;
	try {
		const message = stripUrlQuery(error instanceof Error ? error.message : String(error)).slice(0, 1_000);
		emitErrorLog({
			eventName: 'exception',
			body: message,
			attributes: {
				'exception.type': error instanceof Error ? error.name : typeof error,
				'exception.message': message,
				'exception.stacktrace': error instanceof Error && error.stack ? stripUrlQuery(error.stack).slice(0, 4_000) : undefined,
				'http.route': route,
				'error.source': source
			}
		});
	} catch {
		// Telemetry must never affect the app.
	}
}
