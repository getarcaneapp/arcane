<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { m } from '$lib/paraglide/messages';
	import { createBackendLogsWebSocket, ReconnectingWebSocket } from '$lib/utils/ws';
	import { diagnosticsService } from '$lib/services/diagnostics-service';
	import type { LogEntry } from '$lib/types/diagnostics';
	import { cn } from '$lib/utils';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { TrashIcon } from '$lib/icons';
	import { attrsText } from './diagnostic-log-formatting';

	interface Props {
		height?: string;
		maxLines?: number;
	}

	let { height = '360px', maxLines = 1000 }: Props = $props();

	let logs = $state<LogEntry[]>([]);
	let connected = $state(false);
	let autoScroll = $state(true);
	let filterText = $state('');
	let levelFilter = $state<'all' | 'error' | 'warn' | 'info' | 'debug'>('all');
	let viewport = $state<HTMLElement>();
	let ws: ReconnectingWebSocket<LogEntry> | null = null;

	const filtered = $derived(
		logs.filter((l) => {
			const lvl = l.level.toLowerCase();
			if (levelFilter !== 'all' && !lvl.startsWith(levelFilter)) return false;
			if (filterText && !l.message.toLowerCase().includes(filterText.toLowerCase())) return false;
			return true;
		})
	);

	function scrollToBottom() {
		if (autoScroll && viewport) {
			requestAnimationFrame(() => {
				if (viewport) viewport.scrollTop = viewport.scrollHeight;
			});
		}
	}

	function push(entry: LogEntry) {
		logs.push(entry);
		if (logs.length > maxLines) logs = logs.slice(logs.length - maxLines);
		scrollToBottom();
	}

	function levelClass(level: string): string {
		const l = level.toLowerCase();
		if (l.startsWith('error')) return 'text-red-400';
		if (l.startsWith('warn')) return 'text-amber-400';
		if (l.startsWith('debug')) return 'text-zinc-500';
		return 'text-emerald-400';
	}

	const timeFmt = new Intl.DateTimeFormat('en-US', {
		hour: '2-digit',
		minute: '2-digit',
		second: '2-digit',
		hour12: false
	});
	function fmtTime(t: string): string {
		const d = new Date(t);
		return isNaN(d.getTime()) ? t : timeFmt.format(d);
	}

	function logKey(entry: LogEntry): string {
		return `${entry.time}-${entry.level}-${entry.message}-${attrsText(entry.attrs)}`;
	}

	onMount(async () => {
		try {
			const recent = await diagnosticsService.getRecentLogs();
			logs = recent.slice(-maxLines);
		} catch {
			// initial backlog is best-effort; the live stream fills in
		}
		ws = createBackendLogsWebSocket({
			onMessage: (e) => push(e),
			onOpen: () => (connected = true),
			onClose: () => (connected = false)
		});
		await ws.connect();
		requestAnimationFrame(() => {
			if (viewport) viewport.scrollTop = viewport.scrollHeight;
		});
	});

	onDestroy(() => ws?.close());
</script>

<div class="border-border/60 overflow-hidden rounded-xl border">
	<div class="border-border/60 bg-muted/30 flex flex-wrap items-center gap-2 border-b px-3 py-2">
		<span class="flex items-center gap-1.5 text-xs font-medium">
			<span class={cn('size-2 rounded-full', connected ? 'bg-emerald-500' : 'bg-zinc-500')}></span>
			{connected ? m.diagnostics_logs_streaming() : m.diagnostics_logs_disconnected()}
		</span>
		<span class="text-muted-foreground text-xs tabular-nums">{m.diagnostics_logs_count({ count: filtered.length })}</span>

		<div class="ml-auto flex items-center gap-2">
			<select
				bind:value={levelFilter}
				class="border-border/60 bg-background h-7 rounded-md border px-2 text-xs"
				aria-label={m.diagnostics_logs_all_levels()}
			>
				<option value="all">{m.diagnostics_logs_all_levels()}</option>
				<option value="error">{m.diagnostics_logs_level_error()}</option>
				<option value="warn">{m.diagnostics_logs_level_warn()}</option>
				<option value="info">{m.diagnostics_logs_level_info()}</option>
				<option value="debug">{m.diagnostics_logs_level_debug()}</option>
			</select>
			<input
				bind:value={filterText}
				placeholder={m.diagnostics_logs_filter_placeholder()}
				class="border-border/60 bg-background h-7 w-32 rounded-md border px-2 text-xs"
			/>
			<label class="text-muted-foreground flex items-center gap-1 text-xs">
				<input type="checkbox" bind:checked={autoScroll} class="size-3.5" />
				{m.diagnostics_logs_auto_scroll()}
			</label>
			<ArcaneButton
				action="base"
				tone="ghost"
				size="sm"
				icon={TrashIcon}
				customLabel={m.diagnostics_logs_clear()}
				onclick={() => (logs = [])}
			/>
		</div>
	</div>

	<div
		bind:this={viewport}
		class="bg-background overflow-y-auto px-3 py-2 font-mono text-xs leading-relaxed"
		style="height: {height};"
		role="log"
		aria-live={connected ? 'polite' : 'off'}
	>
		{#if filtered.length === 0}
			<div class="text-muted-foreground py-6 text-center">{m.diagnostics_logs_empty()}</div>
		{:else}
			{#each filtered as entry (logKey(entry))}
				<div class="hover:bg-foreground/5 flex gap-2 rounded px-1 py-0.5">
					<span class="text-muted-foreground shrink-0 tabular-nums">{fmtTime(entry.time)}</span>
					<span class={cn('w-12 shrink-0 font-semibold uppercase', levelClass(entry.level))}>{entry.level}</span>
					<span class="min-w-0 break-words whitespace-pre-wrap">
						{entry.message}
						{#if entry.attrs}<span class="text-muted-foreground ml-2">{attrsText(entry.attrs)}</span>{/if}
					</span>
				</div>
			{/each}
		{/if}
	</div>
</div>
