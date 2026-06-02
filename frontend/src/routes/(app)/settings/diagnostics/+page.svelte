<script lang="ts">
	import { onMount } from 'svelte';
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import { createDiagnosticsWebSocket, ReconnectingWebSocket } from '$lib/utils/ws';
	import { createVisibilityPoller, type VisibilityPoller } from '$lib/utils/visibility-poller';
	import { diagnosticsService } from '$lib/services/diagnostics-service';
	import type { Diagnostics, PprofProfile } from '$lib/types/diagnostics';
	import {
		ActivityIcon,
		CpuIcon,
		MemoryStickIcon,
		ClockIcon,
		ConnectionIcon,
		RefreshIcon,
		DownloadIcon,
		ArrowDownIcon
	} from '$lib/icons';
	import DiagnosticStat from './diagnostic-stat.svelte';
	import DiagnosticLogPanel from './diagnostic-log-panel.svelte';

	let {}: PageProps = $props();

	let diag = $state<Diagnostics | null>(null);
	let connected = $state(false);
	let paused = $state(false);
	let lastUpdated = $state(0);
	let now = $state(Date.now());
	let error = $state<string | null>(null);

	let ws: ReconnectingWebSocket<Diagnostics> | null = null;
	let tickPoller: VisibilityPoller | null = null;

	const wsKindLabels: Record<string, string> = {
		project_logs: m.diagnostics_ws_project_logs(),
		container_logs: m.diagnostics_ws_container_logs(),
		container_stats: m.diagnostics_ws_container_stats(),
		container_exec: m.diagnostics_ws_kind_terminal(),
		system_stats: m.diagnostics_ws_system_stats(),
		service_logs: m.diagnostics_ws_service_logs()
	};

	function fmtBytes(n: number): string {
		if (!n) return '0 B';
		if (n < 1024) return `${n} B`;
		const units = ['KB', 'MB', 'GB', 'TB'];
		let v = n;
		let i = -1;
		while (v >= 1024 && i < units.length - 1) {
			v /= 1024;
			i++;
		}
		return `${v.toFixed(1)} ${units[i]}`;
	}

	function fmtUptime(s: number): string {
		if (s < 60) return `${s}s`;
		const d = Math.floor(s / 86400);
		const h = Math.floor((s % 86400) / 3600);
		const mins = Math.floor((s % 3600) / 60);
		if (d > 0) return `${d}d ${h}h`;
		if (h > 0) return `${h}h ${mins}m`;
		return `${mins}m ${s % 60}s`;
	}

	function fmtNum(n: number): string {
		return n?.toLocaleString() ?? '0';
	}

	function fmtMs(ns: number): string {
		const ms = ns / 1e6;
		return ms < 1 ? `${(ns / 1e3).toFixed(0)}µs` : `${ms.toFixed(2)}ms`;
	}

	const agoText = $derived.by(() => {
		if (!lastUpdated) return '—';
		const s = Math.max(0, Math.round((now - lastUpdated) / 1000));
		return s <= 1 ? m.diagnostics_just_now() : m.diagnostics_seconds_ago({ seconds: s });
	});

	const totalConnections = $derived.by(() => {
		const s = diag?.websocket?.snapshot;
		if (!s) return 0;
		return s.projectLogsActive + s.containerLogsActive + s.containerStats + s.containerExec + s.systemStats + s.serviceLogsActive;
	});

	const heapBar = $derived.by(() => {
		const mem = diag?.memory;
		if (!mem || !mem.heapSys) return null;
		const released = mem.heapReleased;
		const idle = Math.max(0, mem.heapIdle - released);
		const inuse = mem.heapInuse;
		const total = mem.heapSys || inuse + idle + released || 1;
		return {
			inuse: (inuse / total) * 100,
			idle: (idle / total) * 100,
			released: (released / total) * 100
		};
	});

	const maxPause = $derived.by(() => {
		const p = diag?.gc?.recentPausesNs ?? [];
		return p.length ? Math.max(...p) : 0;
	});

	function applySnapshot(d: Diagnostics) {
		diag = d;
		lastUpdated = Date.now();
		error = null;
	}

	function openStream() {
		ws = createDiagnosticsWebSocket({
			onMessage: applySnapshot,
			onOpen: () => (connected = true),
			onClose: () => (connected = false)
		});
		ws.connect();
	}

	function closeStream() {
		ws?.close();
		ws = null;
		connected = false;
	}

	function togglePause() {
		paused = !paused;
		if (paused) closeStream();
		else openStream();
	}

	async function refresh() {
		try {
			applySnapshot(await diagnosticsService.getDiagnostics());
		} catch (e) {
			error = e instanceof Error ? e.message : m.diagnostics_error_load();
		}
	}

	// --- pprof dumps (inline) ---
	let dumpOpen = $state<{ goroutine: boolean; heap: boolean }>({ goroutine: false, heap: false });
	let dumpText = $state<{ goroutine: string; heap: string }>({ goroutine: '', heap: '' });
	let dumpLoading = $state<{ goroutine: boolean; heap: boolean }>({ goroutine: false, heap: false });

	async function loadDump(name: 'goroutine' | 'heap') {
		dumpLoading[name] = true;
		try {
			dumpText[name] = await diagnosticsService.getDump(name);
		} catch (e) {
			dumpText[name] = e instanceof Error ? e.message : m.diagnostics_error_dump();
		} finally {
			dumpLoading[name] = false;
		}
	}

	function onDumpToggle(name: 'goroutine' | 'heap', open: boolean) {
		dumpOpen[name] = open;
		if (open && !dumpText[name] && !dumpLoading[name]) loadDump(name);
	}

	// --- profile downloads ---
	const profiles: { id: PprofProfile; label: string }[] = [
		{ id: 'heap', label: m.diagnostics_profile_heap() },
		{ id: 'goroutine', label: m.diagnostics_profile_goroutine() },
		{ id: 'allocs', label: m.diagnostics_profile_allocs() },
		{ id: 'block', label: m.diagnostics_profile_block() },
		{ id: 'mutex', label: m.diagnostics_profile_mutex() },
		{ id: 'threadcreate', label: m.diagnostics_profile_threadcreate() },
		{ id: 'profile', label: m.diagnostics_profile_cpu() },
		{ id: 'trace', label: m.diagnostics_profile_trace() }
	];
	let downloading = $state<string | null>(null);

	async function download(p: PprofProfile) {
		downloading = p;
		try {
			await diagnosticsService.downloadProfile(p);
		} catch (e) {
			error = e instanceof Error ? e.message : m.diagnostics_error_download();
		} finally {
			downloading = null;
		}
	}

	onMount(() => {
		refresh();
		openStream();
		tickPoller = createVisibilityPoller({
			intervalMs: 1000,
			poll: () => {
				now = Date.now();
			}
		});
		tickPoller.start();
		return () => {
			closeStream();
			tickPoller?.stop();
			tickPoller = null;
		};
	});
</script>

{#snippet row(label: string, value: string | number)}
	<div class="flex items-center justify-between gap-4 py-1.5">
		<span class="text-muted-foreground text-sm">{label}</span>
		<span class="text-sm font-medium tabular-nums">{value}</span>
	</div>
{/snippet}

{#snippet sectionHeader(title: string, Icon: typeof ActivityIcon)}
	<div class="mb-3 flex items-center gap-2">
		<Icon class="text-muted-foreground size-4" />
		<h2 class="text-sm font-semibold tracking-tight">{title}</h2>
	</div>
{/snippet}

<div class="px-2 py-4 sm:px-6 sm:py-6 lg:px-8">
	<!-- Header -->
	<div class="border-border/50 flex flex-wrap items-center justify-between gap-4 border-b pb-4 sm:pb-6">
		<div class="flex items-start gap-3 sm:gap-4">
			<div
				class="bg-primary/10 text-primary ring-primary/20 flex size-9 shrink-0 items-center justify-center rounded-lg ring-1 sm:size-10"
			>
				<ActivityIcon class="size-4 sm:size-5" />
			</div>
			<div>
				<h1 class="text-xl font-semibold tracking-tight sm:text-2xl">{m.diagnostics_title()}</h1>
				<p class="text-muted-foreground mt-1 text-sm">{m.diagnostics_description()}</p>
			</div>
		</div>

		<div class="flex items-center gap-3">
			<span class="flex items-center gap-1.5 text-xs font-medium">
				<span
					class={cn('size-2 rounded-full', paused ? 'bg-amber-500' : connected ? 'animate-pulse bg-emerald-500' : 'bg-zinc-500')}
				></span>
				{paused ? m.diagnostics_status_paused() : connected ? m.diagnostics_status_live() : m.diagnostics_status_connecting()}
			</span>
			<span class="text-muted-foreground hidden text-xs tabular-nums sm:inline"
				>{m.diagnostics_updated_ago({ ago: agoText })}</span
			>
			<ArcaneButton
				action="base"
				tone="outline"
				size="sm"
				customLabel={paused ? m.diagnostics_resume() : m.diagnostics_pause()}
				onclick={togglePause}
			/>
			<ArcaneButton
				action="base"
				tone="outline"
				size="sm"
				icon={RefreshIcon}
				customLabel={m.diagnostics_refresh()}
				onclick={refresh}
			/>
		</div>
	</div>

	{#if error}
		<div class="border-destructive/40 bg-destructive/10 text-destructive mt-4 rounded-lg border px-3 py-2 text-sm">
			{error}
		</div>
	{/if}

	{#if diag}
		<!-- Metric tiles -->
		<div class="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
			<DiagnosticStat
				label={m.diagnostics_stat_goroutines()}
				value={fmtNum(diag.runtime.goroutines)}
				icon={ActivityIcon}
				accent="text-sky-500"
			/>
			<DiagnosticStat
				label={m.diagnostics_stat_heap_alloc()}
				value={fmtBytes(diag.memory.heapAlloc)}
				icon={MemoryStickIcon}
				accent="text-violet-500"
			/>
			<DiagnosticStat
				label={m.diagnostics_stat_ws_conns()}
				value={fmtNum(totalConnections)}
				icon={ConnectionIcon}
				accent="text-emerald-500"
			/>
			<DiagnosticStat
				label={m.diagnostics_stat_gc_cycles()}
				value={fmtNum(diag.memory.numGc)}
				icon={RefreshIcon}
				accent="text-amber-500"
			/>
			<DiagnosticStat
				label={m.diagnostics_stat_cpu_procs()}
				value={`${diag.runtime.gomaxprocs}/${diag.runtime.numCpu}`}
				icon={CpuIcon}
				accent="text-rose-500"
			/>
			<DiagnosticStat
				label={m.diagnostics_stat_uptime()}
				value={fmtUptime(diag.runtime.uptimeSeconds)}
				icon={ClockIcon}
				accent="text-teal-500"
			/>
		</div>

		<div class="mt-8 grid gap-8 lg:grid-cols-2">
			<!-- Runtime -->
			<section>
				{@render sectionHeader(m.diagnostics_section_runtime(), CpuIcon)}
				<div class="divide-border/40 divide-y">
					{@render row(m.diagnostics_runtime_go_version(), diag.runtime.goVersion)}
					{@render row(m.diagnostics_runtime_platform(), `${diag.runtime.os}/${diag.runtime.arch}`)}
					{@render row(m.diagnostics_runtime_gomaxprocs(), diag.runtime.gomaxprocs)}
					{@render row(m.diagnostics_runtime_num_cpu(), diag.runtime.numCpu)}
					{@render row(m.diagnostics_runtime_goroutines(), fmtNum(diag.runtime.goroutines))}
					{@render row(m.diagnostics_runtime_ws_workers(), fmtNum(diag.runtime.wsWorkerGoroutines))}
					{@render row(m.diagnostics_runtime_cgo_calls(), fmtNum(diag.runtime.numCgoCall))}
					{@render row(m.diagnostics_runtime_uptime(), fmtUptime(diag.runtime.uptimeSeconds))}
				</div>
			</section>

			<!-- Memory -->
			<section>
				{@render sectionHeader(m.diagnostics_section_memory(), MemoryStickIcon)}
				{#if heapBar}
					<div class="bg-muted/40 mb-3 flex h-2.5 overflow-hidden rounded-full">
						<div class="bg-violet-500" style="width: {heapBar.inuse}%" title={m.diagnostics_mem_in_use()}></div>
						<div class="bg-violet-500/40" style="width: {heapBar.idle}%" title={m.diagnostics_mem_idle()}></div>
						<div class="bg-zinc-500/40" style="width: {heapBar.released}%" title={m.diagnostics_mem_released()}></div>
					</div>
					<div class="text-muted-foreground mb-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px]">
						<span
							><span class="mr-1 inline-block size-2 rounded-full bg-violet-500"></span>{m.diagnostics_mem_in_use()}
							{fmtBytes(diag.memory.heapInuse)}</span
						>
						<span
							><span class="mr-1 inline-block size-2 rounded-full bg-violet-500/40"></span>{m.diagnostics_mem_idle()}
							{fmtBytes(diag.memory.heapIdle - diag.memory.heapReleased)}</span
						>
						<span
							><span class="mr-1 inline-block size-2 rounded-full bg-zinc-500/40"></span>{m.diagnostics_mem_released()}
							{fmtBytes(diag.memory.heapReleased)}</span
						>
					</div>
				{/if}
				<div class="divide-border/40 divide-y">
					{@render row(m.diagnostics_mem_heap_alloc(), fmtBytes(diag.memory.heapAlloc))}
					{@render row(m.diagnostics_mem_heap_sys(), fmtBytes(diag.memory.heapSys))}
					{@render row(m.diagnostics_mem_heap_objects(), fmtNum(diag.memory.heapObjects))}
					{@render row(m.diagnostics_mem_stack_in_use(), fmtBytes(diag.memory.stackInuse))}
					{@render row(m.diagnostics_mem_total_alloc(), fmtBytes(diag.memory.totalAlloc))}
					{@render row(m.diagnostics_mem_sys_total(), fmtBytes(diag.memory.sys))}
					{@render row(m.diagnostics_mem_next_gc(), fmtBytes(diag.memory.nextGc))}
					{@render row(m.diagnostics_mem_gc_cpu_fraction(), `${(diag.memory.gcCpuFraction * 100).toFixed(3)}%`)}
				</div>
			</section>

			<!-- GC -->
			<section>
				{@render sectionHeader(m.diagnostics_section_gc(), RefreshIcon)}
				<div class="divide-border/40 divide-y">
					{@render row(m.diagnostics_gc_total_cycles(), fmtNum(diag.gc.numGc))}
					{@render row(m.diagnostics_gc_forced_cycles(), fmtNum(diag.memory.numForcedGc))}
					{@render row(m.diagnostics_gc_total_pause(), fmtMs(diag.gc.pauseTotalNs))}
					{@render row(m.diagnostics_gc_last(), diag.gc.lastGc ? new Date(diag.gc.lastGc).toLocaleTimeString() : '—')}
				</div>
				{#if diag.gc.recentPausesNs?.length}
					<div class="mt-3">
						<div class="text-muted-foreground mb-1 text-[11px]">{m.diagnostics_gc_recent_pauses()}</div>
						<div class="flex h-12 items-end gap-0.5">
							{#each diag.gc.recentPausesNs as p, i (i)}
								<div
									class="bg-amber-500/70 flex-1 rounded-sm"
									style="height: {maxPause ? Math.max(4, (p / maxPause) * 100) : 4}%"
									title={fmtMs(p)}
								></div>
							{/each}
						</div>
					</div>
				{/if}
			</section>

			<!-- WebSocket -->
			<section>
				{@render sectionHeader(m.diagnostics_section_websocket(), ConnectionIcon)}
				<div class="divide-border/40 divide-y">
					{@render row(m.diagnostics_ws_project_logs(), fmtNum(diag.websocket.snapshot.projectLogsActive))}
					{@render row(m.diagnostics_ws_container_logs(), fmtNum(diag.websocket.snapshot.containerLogsActive))}
					{@render row(m.diagnostics_ws_container_stats(), fmtNum(diag.websocket.snapshot.containerStats))}
					{@render row(m.diagnostics_ws_terminals(), fmtNum(diag.websocket.snapshot.containerExec))}
					{@render row(m.diagnostics_ws_system_stats(), fmtNum(diag.websocket.snapshot.systemStats))}
					{@render row(m.diagnostics_ws_service_logs(), fmtNum(diag.websocket.snapshot.serviceLogsActive))}
				</div>
			</section>
		</div>

		<!-- Active connections table -->
		{#if diag.websocket.connections?.length}
			<section class="mt-8">
				{@render sectionHeader(m.diagnostics_section_connections({ count: diag.websocket.connections.length }), ConnectionIcon)}
				<div class="border-border/60 overflow-x-auto rounded-xl border">
					<table class="w-full text-left text-sm">
						<thead class="bg-muted/30 text-muted-foreground text-xs">
							<tr>
								<th class="px-3 py-2 font-medium">{m.diagnostics_conn_kind()}</th>
								<th class="px-3 py-2 font-medium">{m.diagnostics_conn_resource()}</th>
								<th class="px-3 py-2 font-medium">{m.diagnostics_conn_client_ip()}</th>
								<th class="px-3 py-2 font-medium">{m.diagnostics_conn_user()}</th>
								<th class="px-3 py-2 font-medium">{m.diagnostics_conn_since()}</th>
							</tr>
						</thead>
						<tbody class="divide-border/40 divide-y">
							{#each diag.websocket.connections as c (c.id)}
								<tr class="hover:bg-muted/20">
									<td class="px-3 py-2">{wsKindLabels[c.kind] ?? c.kind}</td>
									<td class="text-muted-foreground px-3 py-2 font-mono text-xs">{c.resourceId || '—'}</td>
									<td class="text-muted-foreground px-3 py-2 font-mono text-xs">{c.clientIp || '—'}</td>
									<td class="text-muted-foreground px-3 py-2 text-xs">{c.userId || '—'}</td>
									<td class="text-muted-foreground px-3 py-2 text-xs tabular-nums">
										{c.startedAt ? new Date(c.startedAt).toLocaleTimeString() : '—'}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</section>
		{/if}

		<!-- Live logs -->
		<section class="mt-8">
			{@render sectionHeader(m.diagnostics_section_logs(), ActivityIcon)}
			<DiagnosticLogPanel />
		</section>

		<!-- Dumps -->
		<section class="mt-8">
			{@render sectionHeader(m.diagnostics_section_dumps(), CpuIcon)}
			<div class="space-y-2">
				{#each [{ id: 'goroutine', label: m.diagnostics_dump_goroutine() }, { id: 'heap', label: m.diagnostics_dump_heap() }] as d (d.id)}
					{@const name = d.id as 'goroutine' | 'heap'}
					<Collapsible.Root open={dumpOpen[name]} onOpenChange={(o: boolean) => onDumpToggle(name, o)}>
						<Collapsible.Trigger
							class="border-border/60 bg-card/40 hover:bg-muted/30 flex w-full items-center justify-between rounded-lg border px-3 py-2 text-sm font-medium"
						>
							{d.label}
							<ArrowDownIcon class={cn('size-4 transition-transform', dumpOpen[name] && '-rotate-180')} />
						</Collapsible.Trigger>
						<Collapsible.Content>
							<pre
								class="border-border/60 bg-background mt-1 max-h-96 overflow-auto rounded-lg border p-3 font-mono text-[11px] leading-relaxed">{dumpLoading[
									name
								]
									? m.diagnostics_dump_loading()
									: dumpText[name] || m.diagnostics_dump_empty()}</pre>
						</Collapsible.Content>
					</Collapsible.Root>
				{/each}
			</div>
		</section>

		<!-- Profiles -->
		<section class="mt-8 pb-8">
			{@render sectionHeader(m.diagnostics_section_profiles(), DownloadIcon)}
			<p class="text-muted-foreground mb-3 text-sm">{m.diagnostics_profiles_hint()}</p>
			<div class="flex flex-wrap gap-2">
				{#each profiles as p (p.id)}
					<ArcaneButton
						action="base"
						tone="outline"
						size="sm"
						icon={DownloadIcon}
						customLabel={p.label}
						loading={downloading === p.id}
						disabled={downloading !== null}
						onclick={() => download(p.id)}
					/>
				{/each}
			</div>
		</section>
	{:else if !error}
		<div class="text-muted-foreground py-16 text-center text-sm">{m.diagnostics_loading()}</div>
	{/if}
</div>
