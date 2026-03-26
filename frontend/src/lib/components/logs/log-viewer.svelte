<script lang="ts">
	// Dozzle reference: the grouped row shell and left-side timestamp treatment here were
	// informed by amir20/dozzle's LogItem.vue and GroupedLogItem.vue.
	import { dev } from '$app/environment';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import { ArrowDownIcon, ArrowRightIcon } from '$lib/icons';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { m } from '$lib/paraglide/messages';
	import { ReconnectingWebSocket } from '$lib/utils/ws';
	import { cn } from '$lib/utils';
	import { ansiToHtml } from '$lib/utils/ansi';
	import { onDestroy } from 'svelte';
	import {
		buildLogDisplayEntries,
		tryParseStructuredLog,
		type LogViewerDisplayEntry,
		type LogViewerEntry
	} from './log-viewer.utils';
	import StructuredLogEntry from './structured-log-entry.svelte';

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
		onToggleAutoScroll?: () => void;
		onStart?: () => void;
		onStop?: () => void;
		showParsedJson?: boolean;
		groupAdjacentLines?: boolean;
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
		onToggleAutoScroll,
		onStart,
		onStop,
		showParsedJson = $bindable(false),
		groupAdjacentLines = false
	}: Props = $props();

	let logs: LogViewerEntry[] = $state([]);
	let pending: LogViewerEntry[] = [];
	let flushScheduled = false;
	let seq = 0;

	let dropBefore = $state(0);

	const COMPACT_FACTOR = 2;
	let lastCompactSeq = 0;

	let visibleLogs = $derived.by(() => {
		if (dropBefore === 0) return logs;
		return logs.filter((l) => l.id >= dropBefore);
	});

	let displayLogs = $derived.by(() =>
		buildLogDisplayEntries(visibleLogs, {
			groupAdjacentLines,
			type
		})
	);
	let expandedGroups = $state<Set<number>>(new Set());
	let hasAutoEnabledStructuredView = $state(false);

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

	function resetLogState() {
		logs = [];
		pending = [];
		seq = 0;
		dropBefore = 0;
		lastCompactSeq = 0;
		expandedGroups = new Set();
		hasAutoEnabledStructuredView = false;
	}

	export async function clearLogs(opts?: { hard?: boolean; restart?: boolean }) {
		const hard = opts?.hard === true;

		if (hard) {
			resetLogState();
		} else {
			dropBefore = seq;
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
	function streamKey() {
		return type === 'project' ? (projectId ? `project:${projectId}` : null) : containerId ? `ctr:${containerId}` : null;
	}

	const humanType = $derived(type === 'project' ? m.project() : m.container());
	const selectedTargetId = $derived(type === 'project' ? projectId : containerId);
	const fillParent = $derived(height === '100%');
	const minHeight = $derived(height === '100%' ? '0' : '300px');
	const viewportStyle = $derived(fillParent ? 'min-height: 0;' : `height: ${height}; min-height: ${minHeight};`);

	function buildWebSocketEndpoint(path: string): string {
		const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
		return `${protocol}://${window.location.host}${path}`;
	}

	async function buildLogWsEndpoint(): Promise<string> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const basePath =
			type === 'project'
				? `/api/environments/${envId}/ws/projects/${projectId}/logs`
				: `/api/environments/${envId}/ws/containers/${containerId}/logs`;
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

		// Prevent starting if already streaming or connecting
		if (connecting || (shouldBeStreaming && wsClient)) {
			return;
		}

		// A manual stop/start should reload the backend tail instead of appending
		// onto the previous in-memory session buffer.
		resetLogState();

		connecting = true;
		const sessionId = ++streamSession;
		currentStreamSession = sessionId;
		try {
			shouldBeStreaming = true;
			error = null;
			await startWebSocketStream(sessionId);
			// Only notify after successful start
			isStreaming = true;
			onStart?.();
			return;
		} catch (err) {
			console.error('Failed to start log stream:', err);
			error = m.log_stream_failed_connect({ type: humanType });
			isStreaming = false;
			shouldBeStreaming = false;
			if (currentStreamSession === sessionId) {
				currentStreamSession = 0;
			}
		} finally {
			connecting = false;
		}
	}

	async function startWebSocketStream(sessionId: number) {
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
		const timestamp = logData.timestamp || new Date().toISOString();
		const { isJson, isStructured, parsed } = tryParseStructuredLog(logData.message);

		if (isStructured && !hasAutoEnabledStructuredView && !showParsedJson) {
			hasAutoEnabledStructuredView = true;
			showParsedJson = true;
		}

		pending.push({
			id: seq++,
			timestamp,
			level: logData.level as LogViewerEntry['level'],
			message: logData.message,
			service: logData.service,
			containerId: logData.containerId ?? (type === 'container' ? (containerId ?? undefined) : undefined),
			isJson,
			isStructured,
			parsedJson: parsed
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

	function getLevelClass(level: LogViewerEntry['level']): string {
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
		return colors[Math.abs(hash) % colors.length] ?? 'text-gray-300';
	}

	function isStructuredLogData(value: unknown): value is Record<string, unknown> {
		return !!value && typeof value === 'object' && !Array.isArray(value);
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
			currentStreamKey = key;

			if (wasStreaming) {
				(async () => {
					await stopLogStream(false);
					logs = [];
					hasAutoEnabledStructuredView = false;
					if (currentStreamKey === key) {
						startLogStream();
					}
				})();
			} else {
				logs = [];
				hasAutoEnabledStructuredView = false;
			}
		} else if (!currentStreamKey) {
			// First time - just set the key, don't auto-start
			currentStreamKey = key;
		}
	});

	function getDisplayEntryLevel(displayEntry: LogViewerDisplayEntry): LogViewerEntry['level'] {
		return displayEntry.entries[0]?.level ?? 'stdout';
	}

	function getDisplayEntryService(displayEntry: LogViewerDisplayEntry): string | undefined {
		return displayEntry.entries[0]?.service;
	}

	function getDisplayEntryTimestamp(displayEntry: LogViewerDisplayEntry): string | undefined {
		return displayEntry.entries[0]?.timestamp;
	}

	const logTimestampFormatter = new Intl.DateTimeFormat('en-US', {
		month: '2-digit',
		day: '2-digit',
		year: 'numeric',
		hour: '2-digit',
		minute: '2-digit',
		second: '2-digit',
		hour12: true
	});

	function formatLogTimestamp(timestamp: string | undefined): string {
		if (!timestamp) return '';
		const parsedTimestamp = new Date(timestamp);
		if (Number.isNaN(parsedTimestamp.getTime())) {
			return timestamp;
		}
		return logTimestampFormatter.format(parsedTimestamp);
	}

	function isGroupExpanded(displayEntry: LogViewerDisplayEntry): boolean {
		return expandedGroups.has(displayEntry.id);
	}

	function setGroupExpanded(displayEntry: LogViewerDisplayEntry, open: boolean) {
		const next = new Set(expandedGroups);
		if (open) {
			next.add(displayEntry.id);
		} else {
			next.delete(displayEntry.id);
		}
		expandedGroups = next;
	}

	function getGroupedSummaryCount(displayEntry: LogViewerDisplayEntry): number {
		return Math.max(displayEntry.entries.length - 1, 0);
	}
</script>

{#snippet renderLogMessage(entry: LogViewerEntry, contentClass: string)}
	<div class={contentClass}>
		{#if entry.isStructured && showParsedJson && isStructuredLogData(entry.parsedJson)}
			<StructuredLogEntry data={entry.parsedJson} rawJson={entry.message} />
		{:else}
			{@html ansiToHtml(entry.message)}
		{/if}
	</div>
{/snippet}

{#snippet renderGroupedSummary(displayEntry: LogViewerDisplayEntry, contentClass: string)}
	<div class="flex min-w-0 items-start gap-2">
		<Collapsible.Trigger
			class="focus-visible:border-ring focus-visible:ring-ring/50 inline-flex shrink-0 items-center gap-1 rounded border border-transparent px-1 py-0.5 text-zinc-500 outline-none hover:bg-white/4 focus-visible:ring-[3px]"
		>
			{#if isGroupExpanded(displayEntry)}
				<ArrowDownIcon class="size-4" />
			{:else}
				<ArrowRightIcon class="size-4" />
			{/if}
			<span class="text-[11px] font-medium">+{getGroupedSummaryCount(displayEntry)}</span>
		</Collapsible.Trigger>

		{@render renderLogMessage(displayEntry.entries[0]!, contentClass)}
	</div>
{/snippet}

<div
	class={cn(
		'log-viewer rounded-t-none rounded-b-xl border bg-black text-white',
		fillParent && 'flex h-full min-h-0 flex-col',
		className
	)}
>
	{#if error}
		<div class="border-b border-red-700 bg-red-900/20 p-3 text-sm text-red-200">
			{error}
		</div>
	{/if}

	<div
		bind:this={logContainer}
		class={cn(
			'log-viewer overflow-y-auto rounded-t-none rounded-b-xl border bg-black font-mono text-xs text-white sm:text-sm',
			fillParent && 'min-h-0 flex-1'
		)}
		style={viewportStyle}
		role="log"
		aria-live={isStreaming ? 'polite' : 'off'}
		aria-relevant="additions"
		aria-busy={isStreaming}
		tabindex="-1"
		data-auto-scroll={autoScroll}
		data-is-streaming={isStreaming}
	>
		{#if displayLogs.length === 0}
			<div class="p-4 text-center text-gray-500">
				{#if !selectedTargetId}
					{m.log_viewer_no_selection({ type: humanType })}
				{:else if !isStreaming}
					{m.log_viewer_no_logs_available()}
				{:else}
					{m.log_viewer_waiting_for_logs()}
				{/if}
			</div>
		{:else}
			{#each displayLogs as displayLog, index (displayLog.key)}
				<!-- Mobile view -->
				<div
					class={cn(
						'border-l-2 border-transparent px-3 py-2 transition-colors hover:border-blue-500 hover:bg-gray-900/50 sm:hidden',
						index % 2 === 1 && 'bg-zinc-900/60'
					)}
				>
					<div class="mb-1 flex flex-wrap items-center gap-2 text-xs">
						{#if showTimestamps && getDisplayEntryTimestamp(displayLog)}
							<span
								class="inline-flex shrink-0 self-start rounded-md border border-sky-500/15 bg-zinc-900 px-2 py-1 font-semibold text-sky-400 tabular-nums"
								title={getDisplayEntryTimestamp(displayLog)}
							>
								{formatLogTimestamp(getDisplayEntryTimestamp(displayLog))}
							</span>
						{/if}
						{#if type === 'project' && getDisplayEntryService(displayLog)}
							<span
								class="shrink-0 truncate font-semibold {getServiceColor(getDisplayEntryService(displayLog)!)}"
								title={getDisplayEntryService(displayLog)}
							>
								{getDisplayEntryService(displayLog)}
							</span>
						{/if}
						<span class="shrink-0 {getLevelClass(getDisplayEntryLevel(displayLog))}">
							{getDisplayEntryLevel(displayLog).toUpperCase()}
						</span>
					</div>
					{#if displayLog.grouped}
						<Collapsible.Root
							open={isGroupExpanded(displayLog)}
							onOpenChange={(open: boolean) => setGroupExpanded(displayLog, open)}
						>
							{@render renderGroupedSummary(displayLog, 'text-sm break-words whitespace-pre-wrap text-gray-300')}
							<Collapsible.Content>
								<div class="mt-2 space-y-1.5 border-l border-zinc-800 pl-4">
									{#each displayLog.entries.slice(1) as entry (entry.id)}
										{@render renderLogMessage(entry, 'text-sm break-words whitespace-pre-wrap text-gray-300')}
									{/each}
								</div>
							</Collapsible.Content>
						</Collapsible.Root>
					{:else}
						<div class="flex flex-col gap-1.5">
							{#each displayLog.entries as entry (entry.id)}
								{@render renderLogMessage(entry, 'text-sm break-words whitespace-pre-wrap text-gray-300')}
							{/each}
						</div>
					{/if}
				</div>

				<!-- Desktop view -->
				<div
					class={cn(
						'hidden items-start border-l-2 border-transparent px-3 py-1.5 transition-colors hover:border-blue-500 hover:bg-gray-900/50 sm:flex',
						index % 2 === 1 && 'bg-zinc-900/60'
					)}
				>
					{#if showTimestamps && getDisplayEntryTimestamp(displayLog)}
						<span
							class="mr-3 inline-flex min-w-[178px] shrink-0 self-start rounded-md border border-sky-500/15 bg-zinc-900 px-2.5 py-1 text-[11px] font-semibold text-sky-400 tabular-nums"
							title={getDisplayEntryTimestamp(displayLog)}
						>
							{formatLogTimestamp(getDisplayEntryTimestamp(displayLog))}
						</span>
					{/if}
					{#if type === 'project' && getDisplayEntryService(displayLog)}
						<span
							class="mr-3 max-w-[120px] min-w-[120px] shrink-0 truncate text-xs font-semibold {getServiceColor(
								getDisplayEntryService(displayLog)!
							)}"
							title={getDisplayEntryService(displayLog)}
						>
							{getDisplayEntryService(displayLog)}
						</span>
					{/if}
					<span class="mr-2 min-w-fit shrink-0 pt-1 text-xs {getLevelClass(getDisplayEntryLevel(displayLog))}">
						{getDisplayEntryLevel(displayLog).toUpperCase()}
					</span>
					<div class="min-w-0 flex-1">
						{#if displayLog.grouped}
							<Collapsible.Root
								open={isGroupExpanded(displayLog)}
								onOpenChange={(open: boolean) => setGroupExpanded(displayLog, open)}
							>
								{@render renderGroupedSummary(displayLog, 'break-words whitespace-pre-wrap text-gray-300')}
								<Collapsible.Content>
									<div class="mt-2 space-y-1 border-l border-zinc-800 pl-4">
										{#each displayLog.entries.slice(1) as entry (entry.id)}
											{@render renderLogMessage(entry, 'break-words whitespace-pre-wrap text-gray-300')}
										{/each}
									</div>
								</Collapsible.Content>
							</Collapsible.Root>
						{:else}
							<div class="flex flex-1 flex-col gap-1">
								{#each displayLog.entries as entry (entry.id)}
									{@render renderLogMessage(entry, 'break-words whitespace-pre-wrap text-gray-300')}
								{/each}
							</div>
						{/if}
					</div>
				</div>
			{/each}
		{/if}
	</div>
</div>
