<script lang="ts">
	import { dev } from '$app/environment';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { m } from '$lib/paraglide/messages';
	import { ReconnectingWebSocket } from '$lib/utils/ws';
	import { cn } from '$lib/utils';
	import { ansiToHtml } from '$lib/utils/ansi';
	import { onDestroy } from 'svelte';

	interface LogEntry {
		id: number;
		timestamp: string;
		level: 'stdout' | 'stderr' | 'info' | 'error';
		message: string;
		service?: string;
		containerId?: string;
	}

	interface Props {
		class?: string;
		containerId?: string | null;
		projectId?: string | null;
		type?: 'container' | 'project';
		maxLines?: number;
		autoScroll?: boolean;
		showTimestamps?: boolean;
		height?: string;
		tailLines?: number;
		onClear?: () => void;
		onToggleAutoScroll?: () => void;
		onStart?: () => void;
		onStop?: () => void;
	}

	let {
		class: className,
		containerId = null,
		projectId = null,
		type = 'container',
		maxLines = 1000,
		autoScroll = $bindable(true),
		showTimestamps = false,
		height = '400px',
		tailLines = 100,
		onClear,
		onToggleAutoScroll,
		onStart,
		onStop
	}: Props = $props();

	let logs: LogEntry[] = $state([]);
	let pending: LogEntry[] = [];
	let flushScheduled = false;
	let seq = 0;

	let dropBefore = $state(0);

	const COMPACT_FACTOR = 2;
	let lastCompactSeq = 0;

	let visibleLogs = $derived.by(() => {
		if (dropBefore === 0) return logs;
		return logs.filter((l) => l.id >= dropBefore);
	});

	function maybeCompact() {
		const threshold = maxLines * COMPACT_FACTOR;
		if (logs.length <= threshold) return;
		if (seq - lastCompactSeq < maxLines) return;

		// Keep the most recent maxLines entries, trim older ones
		const keepCount = Math.max(1, maxLines);
		if (logs.length > keepCount) {
			const start = logs.length - keepCount;
			logs = logs.slice(start);
		}

		// mark compaction point and clear dropBefore so future compactions don't rely on it
		lastCompactSeq = seq;
		dropBefore = 0;
	}

	function scheduleFlush() {
		if (flushScheduled) return;
		flushScheduled = true;
		queueMicrotask(() => {
			flushScheduled = false;
			if (!pending.length) return;
			logs = [...logs, ...pending];
			pending = [];
			if (logs.length > maxLines * COMPACT_FACTOR) {
				maybeCompact();
			}
			if (autoScroll && logContainer) {
				requestAnimationFrame(() => {
					if (logContainer) logContainer.scrollTop = logContainer.scrollHeight;
				});
			}
		});
	}

	export function clearLogs(opts?: { hard?: boolean; restart?: boolean }) {
		const hard = opts?.hard === true;

		if (hard) {
			logs = [];
			pending = [];
			seq = 0;
			dropBefore = 0;
			lastCompactSeq = 0;
		} else {
			dropBefore = seq;
		}

		onClear?.();

		if (opts?.restart) {
			stopLogStream();
			startLogStream();
		}
	}

	export function hardClearLogs(restart = false) {
		clearLogs({ hard: true, restart });
	}

	let logContainer: HTMLElement | undefined = $state();
	let isStreaming = $state(false);
	let shouldBeStreaming = $state(false);
	let error: string | null = $state(null);
	let eventSource: EventSource | null = null;
	let wsClient: ReconnectingWebSocket<string> | null = null;
	let currentStreamKey: string | null = null;
	function streamKey() {
		return type === 'project' ? (projectId ? `project:${projectId}` : null) : containerId ? `ctr:${containerId}` : null;
	}

	const humanType = $derived(type === 'project' ? m.project() : m.container());

	function buildWebSocketEndpoint(path: string): string {
		const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
		return `${protocol}://${window.location.host}${path}`;
	}

	async function buildLogWsEndpoint(): Promise<string> {
		const currentEnv = environmentStore.selected;
		const envId = currentEnv?.id || 'local';
		const basePath =
			type === 'project'
				? `/api/environments/${envId}/projects/${projectId}/logs/ws`
				: `/api/environments/${envId}/containers/${containerId}/logs/ws`;
		return buildWebSocketEndpoint(`${basePath}?follow=true&tail=${tailLines}&timestamps=true&format=json&batched=true`);
	}

	export async function startLogStream() {
		const targetId = type === 'project' ? projectId : containerId;

		if (!targetId) {
			error = type === 'project' ? m.log_stream_no_project_selected() : m.log_stream_no_container_selected();
			isStreaming = false;
			shouldBeStreaming = false;
			return;
		}

		// Prevent starting if already streaming
		if (shouldBeStreaming && wsClient) {
			return;
		}

		try {
			shouldBeStreaming = true;
			error = null;
			await startWebSocketStream();
			// Only notify after successful start
			isStreaming = true;
			onStart?.();
			return;
		} catch (err) {
			console.error('Failed to start log stream:', err);
			error = m.log_stream_failed_connect({ type: humanType });
			isStreaming = false;
			shouldBeStreaming = false;
		}
	}

	async function startWebSocketStream() {
		// Close existing connection if any
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
				if (dev) console.log(m.log_viewer_connected({ type: humanType }));
				error = null;
				isStreaming = true;
			},
			onMessage: (payload) => {
				if (!payload) return;
				if (Array.isArray(payload)) {
					for (const obj of payload) processLogObject(obj);
				} else if (typeof payload === 'object') {
					processLogObject(payload);
				}
			},
			onError: (e) => {
				console.error('WebSocket log stream error:', e);
				error = m.log_stream_connection_lost({ type: humanType });
			},
			onClose: () => {
				isStreaming = false;
				if (!error && shouldBeStreaming) {
					error = m.log_stream_closed_by_server({ type: humanType });
				}
			},
			shouldReconnect: () => shouldBeStreaming,
			maxBackoff: 10000
		});

		await wsClient.connect();
	}

	function processLogObject(obj: any) {
		if (!obj || typeof obj !== 'object') return;
		const { level = 'stdout', message = '', timestamp = new Date().toISOString(), service, containerId } = obj;

		addLogEntry({
			level,
			message,
			timestamp,
			service,
			containerId
		});
	}

	export function stopLogStream(notifyCallback = true) {
		shouldBeStreaming = false;

		if (eventSource) {
			if (dev) console.log(m.log_viewer_stopping({ type: humanType }));
			eventSource.close();
			eventSource = null;
		}
		if (wsClient) {
			try {
				wsClient.close();
			} catch {}
			wsClient = null;
		}
		isStreaming = false;
		if (notifyCallback) {
			onStop?.();
		}
	}

	function addLogEntry(logData: { level: string; message: string; timestamp?: string; service?: string; containerId?: string }) {
		const timestamp = logData.timestamp || new Date().toISOString();
		pending.push({
			id: seq++,
			timestamp,
			level: logData.level as LogEntry['level'],
			message: logData.message,
			service: logData.service,
			containerId: logData.containerId
		});
		scheduleFlush();
	}

	onDestroy(() => {
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

	function getLevelClass(level: LogEntry['level']): string {
		switch (level) {
			case 'stderr':
			case 'error':
				return 'text-red-400';
			case 'stdout':
			case 'info':
				return 'text-green-400';
			default:
				return 'text-gray-300';
		}
	}

	// Generate consistent color for service names (similar to docker compose)
	function getServiceColor(service: string): string {
		const colors = [
			'text-cyan-400',
			'text-yellow-400',
			'text-green-400',
			'text-blue-400',
			'text-purple-400',
			'text-pink-400',
			'text-orange-400',
			'text-teal-400',
			'text-lime-400',
			'text-indigo-400',
			'text-fuchsia-400',
			'text-rose-400'
		];

		// Simple hash function to consistently map service names to colors
		let hash = 0;
		for (let i = 0; i < service.length; i++) {
			hash = service.charCodeAt(i) + ((hash << 5) - hash);
		}
		return colors[Math.abs(hash) % colors.length];
	}

	$effect(() => {
		const key = streamKey();
		if (!key) {
			if (currentStreamKey && shouldBeStreaming) {
				stopLogStream(false);
			}
			currentStreamKey = null;
			return;
		}

		// If key changed while streaming, restart with new key
		if (currentStreamKey && currentStreamKey !== key) {
			const wasStreaming = shouldBeStreaming;
			if (wasStreaming) {
				stopLogStream(false);
			}
			logs = [];
			currentStreamKey = key;
			if (wasStreaming) {
				startLogStream();
			}
		} else if (!currentStreamKey) {
			// First time - just set the key, don't auto-start
			currentStreamKey = key;
		}
	});
</script>

<div class={cn('log-viewer rounded-t-none rounded-b-xl border bg-black text-white', className)}>
	{#if error}
		<div class="border-b border-red-700 bg-red-900/20 p-3 text-sm text-red-200">
			{error}
		</div>
	{/if}

	<div
		bind:this={logContainer}
		class="log-viewer overflow-y-auto rounded-t-none rounded-b-xl border bg-black font-mono text-xs text-white sm:text-sm"
		style="height: {height}; min-height: 300px;"
		role="log"
		aria-live={isStreaming ? 'polite' : 'off'}
		aria-relevant="additions"
		aria-busy={isStreaming}
		tabindex="-1"
		data-auto-scroll={autoScroll}
		data-is-streaming={isStreaming}
	>
		{#if logs.length === 0}
			<div class="p-4 text-center text-gray-500">
				{#if !containerId}
					{m.log_viewer_no_selection({ type: humanType })}
				{:else if !isStreaming}
					{m.log_viewer_no_logs_available()}
				{:else}
					{m.log_viewer_waiting_for_logs()}
				{/if}
			</div>
		{:else}
			{#each visibleLogs as log (log.id)}
				<!-- Mobile view -->
				<div
					class="border-l-2 border-transparent px-3 py-2 transition-colors hover:border-blue-500 hover:bg-gray-900/50 sm:hidden"
				>
					<div class="mb-1 flex items-center gap-2 text-xs">
						{#if type === 'project' && log.service}
							<span class="shrink-0 truncate font-semibold {getServiceColor(log.service)}" title={log.service}>
								{log.service}
							</span>
						{/if}
						<span class="shrink-0 {getLevelClass(log.level)}">
							{log.level.toUpperCase()}
						</span>
					</div>
					<div class="text-sm break-words whitespace-pre-wrap text-gray-300">
						{@html ansiToHtml(log.message)}
					</div>
				</div>

				<!-- Desktop view -->
				<div
					class="hidden border-l-2 border-transparent px-3 py-1 transition-colors hover:border-blue-500 hover:bg-gray-900/50 sm:flex"
				>
					{#if type === 'project' && log.service}
						<span
							class="mr-3 max-w-[120px] min-w-[120px] shrink-0 truncate text-xs font-semibold {getServiceColor(log.service)}"
							title={log.service}
						>
							{log.service}
						</span>
					{/if}
					<span class="mr-2 shrink-0 text-xs {getLevelClass(log.level)} min-w-fit">
						{log.level.toUpperCase()}
					</span>
					<span class="flex-1 break-words whitespace-pre-wrap text-gray-300">
						{@html ansiToHtml(log.message)}
					</span>
				</div>
			{/each}
		{/if}
	</div>
</div>
