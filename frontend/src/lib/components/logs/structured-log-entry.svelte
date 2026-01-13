<script lang="ts">
	import { CopyIcon, CheckIcon } from '$lib/icons';

	interface Props {
		data: Record<string, unknown>;
		rawJson?: string;
		showTimestamp?: boolean;
	}

	let { data, rawJson, showTimestamp = false }: Props = $props();

	let showDetails = $state(false);
	let copied = $state(false);

	// Common field names used by various structured logging frameworks
	// Supports: Rust (tracing, slog), Go (zap, zerolog, logrus), Node (pino, winston, bunyan),
	// Python (structlog), Java (Log4j2), .NET (Serilog), and more
	const LEVEL_KEYS = ['level', 'severity', 'log.level', 'loglevel', 'lvl', 'levelname', 'priority'];
	const MESSAGE_KEYS = ['message', 'msg', 'text', 'body', 'event', 'log', 'content'];
	const TIMESTAMP_KEYS = ['timestamp', 'time', 'ts', '@timestamp', 'datetime', 'date', 'created', 'time_local'];
	const TARGET_KEYS = ['target', 'logger', 'name', 'source', 'component', 'module', 'log.logger', 'logger_name', 'category'];
	const ERROR_KEYS = ['error', 'err', 'exception', 'stack', 'stacktrace', 'error.message'];
	const TRACE_KEYS = ['trace_id', 'traceId', 'correlation_id', 'correlationId', 'request_id', 'requestId', 'spanId', 'span_id'];
	
	// Nested message location (e.g., tracing-subscriber in Rust uses fields.message)
	const NESTED_MESSAGE_PATHS = [
		['fields', 'message'],
		['fields', 'msg'],
		['data', 'message']
	];

	function findValue(obj: Record<string, unknown>, keys: string[]): unknown {
		for (const key of keys) {
			if (key in obj) return obj[key];
			// Handle dot notation keys like 'log.level'
			if (key.includes('.')) {
				const parts = key.split('.');
				let current: unknown = obj;
				for (const part of parts) {
					if (current && typeof current === 'object' && part in (current as Record<string, unknown>)) {
						current = (current as Record<string, unknown>)[part];
					} else {
						current = undefined;
						break;
					}
				}
				if (current !== undefined) return current;
			}
		}
		return undefined;
	}

	function findNestedMessage(obj: Record<string, unknown>): string | undefined {
		for (const path of NESTED_MESSAGE_PATHS) {
			let current: unknown = obj;
			for (const key of path) {
				if (current && typeof current === 'object' && key in (current as Record<string, unknown>)) {
					current = (current as Record<string, unknown>)[key];
				} else {
					current = undefined;
					break;
				}
			}
			if (typeof current === 'string') return current;
		}
		return undefined;
	}

	function normalizeLevelValue(level: unknown): string | undefined {
		if (typeof level === 'string') return level;
		if (typeof level === 'number') {
			// Syslog levels: 0=emerg, 1=alert, 2=crit, 3=error, 4=warn, 5=notice, 6=info, 7=debug
			const syslogMap: Record<number, string> = {
				0: 'fatal',
				1: 'fatal',
				2: 'fatal',
				3: 'error',
				4: 'warn',
				5: 'info',
				6: 'info',
				7: 'debug'
			};
			return syslogMap[level] ?? 'info';
		}
		return undefined;
	}

	function normalizeTimestamp(ts: unknown): string | undefined {
		if (typeof ts === 'string') return ts;
		if (typeof ts === 'number') {
			// Handle Unix timestamps (seconds or milliseconds)
			const timestamp = ts > 1e12 ? ts : ts * 1000; // Convert to ms if in seconds
			return new Date(timestamp).toISOString();
		}
		return undefined;
	}

	function extractFields(obj: Record<string, unknown>) {
		const rawLevel = findValue(obj, LEVEL_KEYS);
		let message = findValue(obj, MESSAGE_KEYS);

		// Try nested paths if no top-level message found
		if (!message) {
			message = findNestedMessage(obj);
		}

		// Fallback: use 'event' field if no message found
		if (!message && obj.event) {
			message = String(obj.event);
		}

		const rawTimestamp = findValue(obj, TIMESTAMP_KEYS);
		const target = findValue(obj, TARGET_KEYS);
		const error = findValue(obj, ERROR_KEYS);
		const traceId = findValue(obj, TRACE_KEYS);

		return {
			level: normalizeLevelValue(rawLevel),
			message: typeof message === 'string' ? message : undefined,
			timestamp: normalizeTimestamp(rawTimestamp),
			target: typeof target === 'string' ? target : undefined,
			error: error !== undefined ? error : undefined,
			traceId: typeof traceId === 'string' ? traceId : undefined,
			hasStructure: !!(rawLevel || message || rawTimestamp || target)
		};
	}

	function getLevelColor(level: string | undefined): string {
		if (!level) return 'text-gray-400';
		const l = level.toLowerCase();
		if (l === 'error' || l === 'err' || l === 'fatal' || l === 'critical' || l === 'panic' || l === 'emergency') return 'text-red-400';
		if (l === 'warn' || l === 'warning' || l === 'alert') return 'text-yellow-400';
		if (l === 'info' || l === 'information' || l === 'notice') return 'text-green-400';
		if (l === 'debug' || l === 'dbg') return 'text-blue-400';
		if (l === 'trace' || l === 'verbose' || l === 'finest') return 'text-purple-400';
		return 'text-gray-400';
	}

	function getLevelBgColor(level: string | undefined): string {
		if (!level) return 'bg-gray-800';
		const l = level.toLowerCase();
		if (l === 'error' || l === 'err' || l === 'fatal' || l === 'critical' || l === 'panic' || l === 'emergency') return 'bg-red-900/30';
		if (l === 'warn' || l === 'warning' || l === 'alert') return 'bg-yellow-900/30';
		if (l === 'info' || l === 'information' || l === 'notice') return 'bg-green-900/30';
		if (l === 'debug' || l === 'dbg') return 'bg-blue-900/30';
		if (l === 'trace' || l === 'verbose' || l === 'finest') return 'bg-purple-900/30';
		return 'bg-gray-800';
	}

	function formatTimestamp(ts: string | undefined): string {
		if (!ts) return '';
		try {
			const date = new Date(ts);
			if (isNaN(date.getTime())) return ts;
			return date.toLocaleTimeString('en-US', {
				hour12: false,
				hour: '2-digit',
				minute: '2-digit',
				second: '2-digit',
				fractionalSecondDigits: 3
			});
		} catch {
			return ts;
		}
	}

	const extracted = $derived(extractFields(data));
	const formattedTime = $derived(formatTimestamp(extracted.timestamp));
	const levelDisplay = $derived(extracted.level?.toUpperCase() ?? 'LOG');
	const jsonString = $derived(rawJson ?? JSON.stringify(data, null, 2));
	
	// For unstructured JSON, create a compact string representation
	const compactJson = $derived(JSON.stringify(data));

	async function copyToClipboard() {
		try {
			await navigator.clipboard.writeText(jsonString);
			copied = true;
			setTimeout(() => {
				copied = false;
			}, 2000);
		} catch (err) {
			console.error('Failed to copy to clipboard:', err);
		}
	}
</script>

<div class="structured-log-entry">
	<!-- Main log line with extracted fields -->
	<div class="flex items-start gap-2">
		<!-- Level badge -->
		<span
			class="shrink-0 rounded px-1.5 py-0.5 text-xs font-semibold {getLevelColor(extracted.level)} {getLevelBgColor(extracted.level)}"
		>
			{levelDisplay}
		</span>

		<!-- Timestamp (if available and enabled) -->
		{#if showTimestamp && formattedTime}
			<span class="shrink-0 text-xs text-gray-500" title={extracted.timestamp}>
				{formattedTime}
			</span>
		{/if}

		<!-- Target/Logger name -->
		{#if extracted.target}
			<span class="shrink-0 text-xs text-cyan-400 font-medium" title={extracted.target}>
				[{extracted.target}]
			</span>
		{/if}

		<!-- Message -->
		<span class="flex-1 text-gray-200">
			{#if extracted.message}
				{extracted.message}
			{:else if !extracted.hasStructure}
				<!-- For unstructured JSON, display as compact string -->
				<span class="text-gray-300">{compactJson}</span>
			{:else}
				<span class="text-gray-500 italic">No message field</span>
			{/if}
		</span>

		<!-- Copy to clipboard button -->
		<button
			type="button"
			onclick={copyToClipboard}
			class="shrink-0 rounded p-1 text-gray-500 hover:text-gray-200 hover:bg-gray-700 transition-colors"
			title={copied ? 'Copied!' : 'Copy JSON'}
		>
			{#if copied}
				<CheckIcon class="size-3.5 text-green-400" />
			{:else}
				<CopyIcon class="size-3.5" />
			{/if}
		</button>

		<!-- Details toggle button - always show to expand full JSON -->
		<button
			type="button"
			onclick={() => (showDetails = !showDetails)}
			class="shrink-0 rounded px-1.5 py-0.5 text-xs text-gray-400 hover:text-gray-200 hover:bg-gray-700 transition-colors"
			title={showDetails ? 'Hide JSON' : 'Show full JSON'}
		>
			{showDetails ? '▼' : '▶'}
		</button>
	</div>

	<!-- Collapsible full JSON section -->
	{#if showDetails}
		<div class="mt-1 ml-4 pl-2 border-l border-gray-700">
			<pre class="text-xs text-gray-300 whitespace-pre-wrap break-all">{JSON.stringify(data, null, 2)}</pre>
		</div>
	{/if}
</div>
