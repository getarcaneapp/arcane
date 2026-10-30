<script lang="ts">
	// Dozzle reference: the grouped row shell and left-side timestamp treatment here were
	// informed by amir20/dozzle's LogItem.vue and GroupedLogItem.vue.
	import { dev } from '$app/env';
	import { onDestroy, untrack } from 'svelte';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { createVirtualizer } from '#lib/components/ui/virtualizer.svelte.js';
	import VirtualRows from '#lib/components/virtual-rows.svelte';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { AlertIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';
	import { ansiToHtml, nowInstantString } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { ReconnectingWebSocket } from '#lib/utils/ws.js';

	import LogRow from './log-row.svelte';
	import { buildLogDisplayEntries, getStructuredLog, type LogViewerDisplayEntry, type LogViewerEntry } from './log-viewer.utils';

	interface Props {
		class?: string;
		containerId?: string | null;
		environmentId: string;
		projectId?: string | null;
		serviceId?: string | null;
		type?: 'container' | 'project' | 'service';
		maxLines?: number;
		autoScroll?: boolean;
		showTimestamps?: boolean;
		height?: string;
		tailLines?: number;
		onToggleAutoScroll?: () => void;
		onStart?: () => void;
		onStop?: () => void;
		showParsedJson?: boolean;
		showStreamLabels?: boolean;
		groupAdjacentLines?: boolean;
		searchTerm?: string;
	}

	let {
		class: className,
		containerId = null,
		environmentId,
		projectId = null,
		serviceId = null,
		type = 'container',
		maxLines = 1000,
		autoScroll = $bindable(true),
		showTimestamps = false,
		height = '400px',
		tailLines = 100,
		onToggleAutoScroll,
		onStart,
		onStop,
		showParsedJson = $bindable(false),
		showStreamLabels = true,
		groupAdjacentLines = false,
		searchTerm = ''
	}: Props = $props();

	const isMobile = new IsMobile();

	let logs = $state.raw<LogViewerEntry[]>([]);
	let pending: LogViewerEntry[] = [];
	let flushFrame: number | null = null;
	let seq = 0;
	let lastAppend: { from: LogViewerEntry[]; to: LogViewerEntry[]; batch: LogViewerEntry[] } | null = null;
	let filterCache: { term: string; source: LogViewerEntry[]; result: LogViewerEntry[] } | null = null;

	const lineCap = $derived(Math.max(1, maxLines));

	function matchesSearch(entry: LogViewerEntry, term: string): boolean {
		return entry.message.toLowerCase().includes(term) || !!entry.service?.toLowerCase().includes(term);
	}

	const visibleLogs = $derived.by(() => {
		const term = searchTerm.trim().toLowerCase();
		const source = logs;
		if (!term) {
			filterCache = null;
			return source;
		}

		const cache = filterCache;
		const append = lastAppend;
		let result: LogViewerEntry[];
		if (cache && append && cache.term === term && cache.source === append.from && append.to === source) {
			const firstId = source[0]?.id ?? 0;
			const added = append.batch.filter((entry) => entry.id >= firstId && matchesSearch(entry, term));
			const start = cache.result.findIndex((entry) => entry.id >= firstId);
			const kept = start === -1 ? [] : cache.result.slice(start);
			result = start === 0 && added.length === 0 ? cache.result : kept.concat(added);
		} else {
			result = source.filter((entry) => matchesSearch(entry, term));
		}

		filterCache = { term, source, result };
		return result;
	});

	const displayLogs = $derived(buildLogDisplayEntries(visibleLogs, { groupAdjacentLines, type }));
	let expandedGroups = $state<Set<number>>(new Set());
	let hasAutoEnabledStructuredView = $state(false);

	function scheduleFlush() {
		if (flushFrame !== null) return;
		flushFrame = requestAnimationFrame(() => {
			flushFrame = null;
			if (!pending.length) return;
			const batch = pending;
			pending = [];
			const merged = logs.concat(batch);
			const next = merged.length > lineCap ? merged.slice(-lineCap) : merged;
			lastAppend = { from: logs, to: next, batch };
			logs = next;
		});
	}

	function cancelFlush() {
		if (flushFrame !== null) cancelAnimationFrame(flushFrame);
		flushFrame = null;
	}

	function clearBuffer() {
		cancelFlush();
		pending = [];
		lastAppend = null;
		logs = [];
	}

	function resetLogState() {
		clearBuffer();
		seq = 0;
		expandedGroups = new Set();
		hasAutoEnabledStructuredView = false;
	}

	export async function clearLogs(opts?: { hard?: boolean; restart?: boolean }) {
		const hard = opts?.hard === true;

		if (hard) {
			resetLogState();
		} else {
			clearBuffer();
		}

		if (opts?.restart) {
			await stopLogStream();
			startLogStream();
		}
	}

	export function hardClearLogs(restart = false) {
		clearLogs({ hard: true, restart });
	}

	let logContainer: HTMLElement | undefined = $state();
	let isStreaming = $state(false);
	let shouldBeStreaming = $state(false);
	let connecting = $state(false);
	let error: string | null = $state(null);
	let eventSource: EventSource | null = null;
	let wsClient: ReconnectingWebSocket<string> | null = null;
	let currentStreamKey: string | null = null;
	let streamSession = 0;
	let currentStreamSession = 0;
	let restartPending = false;

	function streamKey() {
		if (type === 'project') return projectId ? `${environmentId}:project:${projectId}` : null;
		if (type === 'service') return serviceId ? `${environmentId}:svc:${serviceId}` : null;
		return containerId ? `${environmentId}:ctr:${containerId}` : null;
	}

	const humanType = $derived.by(() => {
		if (type === 'project') return m.project();
		if (type === 'service') return m.swarm_service();
		return m.container();
	});
	const targetId = $derived.by(() => {
		if (type === 'project') return projectId;
		if (type === 'service') return serviceId;
		return containerId;
	});
	const logWsBasePath = $derived.by(() => {
		if (type === 'project') return `/api/environments/${environmentId}/ws/projects/${projectId}/logs`;
		if (type === 'service') return `/api/environments/${environmentId}/ws/swarm/services/${serviceId}/logs`;
		return `/api/environments/${environmentId}/ws/containers/${containerId}/logs`;
	});
	const fillParent = $derived(height === '100%');
	const minHeight = $derived(height === '100%' ? '0' : '300px');

	function buildWebSocketEndpoint(path: string): string {
		const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
		return `${protocol}://${window.location.host}${path}`;
	}

	async function buildLogWsEndpoint(): Promise<string> {
		return buildWebSocketEndpoint(`${logWsBasePath}?follow=true&tail=${tailLines}&timestamps=true&format=json&batched=true`);
	}

	export async function startLogStream() {
		if (!targetId) {
			error = type === 'project' ? m.log_stream_no_project_selected() : m.log_stream_no_container_selected();
			isStreaming = false;
			shouldBeStreaming = false;
			return;
		}

		// Prevent starting if already streaming or connecting
		if (connecting || (shouldBeStreaming && wsClient)) {
			return;
		}

		resetLogState();

		connecting = true;
		const sessionId = ++streamSession;
		currentStreamSession = sessionId;
		try {
			const requestResult1 = await tryCatch(
				(async () => {
					shouldBeStreaming = true;
					error = null;
					await startWebSocketStream(sessionId);
					// Only notify after successful start
					isStreaming = true;
					onStart?.();
					return;
				})()
			);
			if (requestResult1.error !== null) {
				const err = requestResult1.error;

				console.error('Failed to start log stream:', err);
				error = m.log_stream_failed_connect({ type: humanType });
				isStreaming = false;
				shouldBeStreaming = false;
				if (currentStreamSession === sessionId) {
					currentStreamSession = 0;
				}
			}
		} finally {
			connecting = false;
		}
	}

	async function startWebSocketStream(sessionId: number) {
		if (wsClient) {
			try {
				wsClient.close();
			} catch {}
			wsClient = null;
		}

		wsClient = new ReconnectingWebSocket<string>({
			buildUrl: async () => {
				return await buildLogWsEndpoint();
			},
			parseMessage: (evt) => {
				if (typeof evt.data !== 'string') return null;
				try {
					return JSON.parse(evt.data);
				} catch {
					return null;
				}
			},
			onOpen: () => {
				if (sessionId !== currentStreamSession) return;
				if (dev) console.log(m.log_viewer_connected({ type: humanType }));
				error = null;
				isStreaming = true;
			},
			onMessage: (payload) => {
				if (sessionId !== currentStreamSession) return;
				if (!payload) return;
				if (Array.isArray(payload)) {
					for (const obj of payload) processLogObject(obj);
				} else if (typeof payload === 'object') {
					processLogObject(payload);
				}
			},
			onError: (e) => {
				if (sessionId !== currentStreamSession) return;
				console.error('WebSocket log stream error:', e);
				error = m.log_stream_connection_lost({ type: humanType });
			},
			onClose: () => {
				if (sessionId !== currentStreamSession) return;
				isStreaming = false;
				if (!error && shouldBeStreaming) {
					error = m.log_stream_closed_by_server({ type: humanType });
				}
			},
			shouldReconnect: () => shouldBeStreaming && sessionId === currentStreamSession,
			maxBackoff: 10000
		});

		await wsClient.connect();
	}

	function processLogObject(obj: unknown) {
		if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return;
		addLogEntry({
			level: 'level' in obj && typeof obj.level === 'string' ? obj.level : 'stdout',
			message: 'message' in obj && typeof obj.message === 'string' ? obj.message : '',
			timestamp: 'timestamp' in obj && typeof obj.timestamp === 'string' ? obj.timestamp : nowInstantString(),
			service: 'service' in obj && typeof obj.service === 'string' ? obj.service : undefined,
			containerId: 'containerId' in obj && typeof obj.containerId === 'string' ? obj.containerId : undefined
		});
	}

	export async function stopLogStream(notifyCallback = true): Promise<void> {
		shouldBeStreaming = false;
		currentStreamSession = 0;

		if (eventSource) {
			if (dev) console.log(m.log_viewer_stopping({ type: humanType }));
			eventSource.close();
			eventSource = null;
		}

		let closePromise: Promise<void> | undefined;
		if (wsClient) {
			closePromise = wsClient.closeAndWait?.();
			wsClient = null;
		}

		isStreaming = false;
		connecting = false;
		if (notifyCallback) {
			onStop?.();
		}

		await closePromise;
	}

	function addLogEntry(logData: { level: string; message: string; timestamp?: string; service?: string; containerId?: string }) {
		const entry: LogViewerEntry = {
			id: seq++,
			timestamp: logData.timestamp || nowInstantString(),
			level: logData.level as LogViewerEntry['level'],
			message: logData.message,
			html: ansiToHtml(logData.message),
			service: logData.service,
			containerId: logData.containerId ?? (type === 'container' ? (containerId ?? undefined) : undefined)
		};

		if (!hasAutoEnabledStructuredView && !showParsedJson && getStructuredLog(entry).isStructured) {
			hasAutoEnabledStructuredView = true;
			showParsedJson = true;
		}

		pending.push(entry);
		if (pending.length > lineCap * 2) pending = pending.slice(-lineCap);
		scheduleFlush();
	}

	onDestroy(() => {
		cancelFlush();
		stopLogStream(false);
	});

	export function toggleAutoScroll() {
		autoScroll = !autoScroll;
		onToggleAutoScroll?.();
	}

	export function getIsStreaming() {
		return isStreaming;
	}

	export function getLogCount() {
		return logs.length;
	}

	// Keep short logs on the plain render path.
	const VIRTUALIZE_THRESHOLD = 100;
	const shouldVirtualize = $derived(displayLogs.length > VIRTUALIZE_THRESHOLD);
	const rowVirtualizer = createVirtualizer<HTMLElement, HTMLDivElement>(() => ({
		count: displayLogs.length,
		getScrollElement: () => logContainer ?? null,
		getItemKey: (index) => displayLogs[index]?.key ?? index,
		estimateSize: () => 32,
		overscan: 12,
		enabled: shouldVirtualize && !!logContainer
	}));

	// Follow new output (and late row measurements) to the bottom while auto-scroll is on.
	$effect(() => {
		void displayLogs;
		void rowVirtualizer.totalSize;
		const container = logContainer;
		if (!container || !untrack(() => autoScroll)) return;
		const frame = requestAnimationFrame(() => {
			container.scrollTop = container.scrollHeight;
		});
		return () => cancelAnimationFrame(frame);
	});

	$effect(() => {
		const key = streamKey();
		const wasStreaming = untrack(() => shouldBeStreaming) || restartPending;
		if (!key) {
			if (currentStreamKey && wasStreaming) {
				stopLogStream(false);
			}
			restartPending = false;
			currentStreamKey = null;
			return;
		}

		// If key changed while streaming, restart with new key
		if (currentStreamKey && currentStreamKey !== key) {
			currentStreamKey = key;

			if (!wasStreaming) {
				untrack(resetLogState);
				return;
			}

			let cancelled = false;
			restartPending = true;
			(async () => {
				await stopLogStream(false);
				if (cancelled) return;
				restartPending = false;
				if (currentStreamKey === key) startLogStream();
			})();
			return () => {
				cancelled = true;
			};
		} else if (!currentStreamKey) {
			// First time - just set the key, don't auto-start
			currentStreamKey = key;
		}
	});

	function setGroupExpanded(displayEntry: LogViewerDisplayEntry, open: boolean) {
		const next = new Set(expandedGroups);
		if (open) {
			next.add(displayEntry.id);
		} else {
			next.delete(displayEntry.id);
		}
		expandedGroups = next;
	}
</script>

{#snippet logRow(displayLog: LogViewerDisplayEntry, index: number)}
	<LogRow
		{displayLog}
		{index}
		mobile={isMobile.current}
		{type}
		{showTimestamps}
		{showParsedJson}
		{showStreamLabels}
		expanded={expandedGroups.has(displayLog.id)}
		onExpandedChange={(open) => setGroupExpanded(displayLog, open)}
	/>
{/snippet}

<div
	class={cn(
		'dark rounded-t-none rounded-b-xl border bg-black text-white',
		fillParent && 'flex h-full min-h-0 flex-col',
		className
	)}
>
	{#if error && displayLogs.length > 0}
		<div class="border-b border-destructive bg-destructive/10 p-3 text-sm text-destructive">
			{error}
		</div>
	{/if}

	<div
		bind:this={logContainer}
		class={cn(
			'overflow-y-auto rounded-t-none rounded-b-xl border bg-black font-mono text-xs text-white sm:text-sm',
			fillParent ? 'min-h-0 flex-1' : 'h-(--log-height) min-h-(--log-min-height)'
		)}
		style={fillParent ? undefined : `--log-height: ${height}; --log-min-height: ${minHeight}`}
		role="log"
		aria-live={isStreaming ? 'polite' : 'off'}
		aria-relevant="additions"
		aria-busy={isStreaming}
		tabindex="-1"
		data-auto-scroll={autoScroll}
		data-is-streaming={isStreaming}
	>
		{#if displayLogs.length === 0}
			{#if error}
				<div class="flex flex-col items-center gap-1 p-4 text-center" role="status">
					<span class="font-semibold text-destructive">{m.common_unavailable()}</span>
					<span class="text-muted-foreground">{error}</span>
				</div>
			{:else}
				<div class="p-4 text-center text-muted-foreground">
					{#if !(containerId || projectId || serviceId)}
						{m.log_viewer_no_selection({ type: humanType })}
					{:else if !isStreaming}
						{m.log_viewer_no_logs_available()}
					{:else}
						{m.log_viewer_waiting_for_logs()}
					{/if}
				</div>
			{/if}
		{:else}
			<svelte:boundary>
				{#if shouldVirtualize}
					<VirtualRows virtualizer={rowVirtualizer} rows={displayLogs} row={logRow} />
				{:else}
					{#each displayLogs as displayLog, index (displayLog.key)}
						{@render logRow(displayLog, index)}
					{/each}
				{/if}

				{#snippet failed(_renderError: unknown, reset: () => void)}
					<div class="p-4">
						<Alert.Root variant="destructive" icon={AlertIcon} heading={m.error_generic()}>
							<div class="mt-3">
								<ArcaneButton action="refresh" size="sm" customLabel={m.common_retry()} onclick={reset} />
							</div>
						</Alert.Root>
					</div>
				{/snippet}
			</svelte:boundary>
		{/if}
	</div>
</div>
