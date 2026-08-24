<script lang="ts">
	import { onMount } from 'svelte';
	import { cn } from '#lib/utils';
	import { m } from '#lib/paraglide/messages';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Collapsible from '#lib/components/ui/collapsible';
	import * as Alert from '#lib/components/ui/alert';
	import * as Table from '#lib/components/ui/table';
	import * as Tabs from '#lib/components/ui/tabs';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import { SettingsPageLayout } from '#lib/layouts';
	import type { SettingsActionButton, SettingsStatCard } from '#lib/layouts/types.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte';
	import { createDiagnosticsWebSocket, ReconnectingWebSocket } from '#lib/utils/ws';
	import { diagnosticsService } from '#lib/services/diagnostics-service';
	import type { Diagnostics, GoroutineLeakReport, PprofProfile } from '#lib/types/diagnostics';
	import {
		ActivityIcon,
		AlertTriangleIcon,
		CpuIcon,
		MemoryStickIcon,
		ClockIcon,
		ConnectionIcon,
		DownloadIcon,
		ArrowDownIcon
	} from '#lib/icons';
	import DiagnosticLogPanel from './diagnostic-log-panel.svelte';
	import DiagnosticLeakPanel from './diagnostic-leak-panel.svelte';
	import { formatTime } from '#lib/utils/formatting';

	let {}: PageProps = $props();

	type DiagnosticsTab = 'overview' | 'connections' | 'logs' | 'profiling';

	let diag = $state<Diagnostics | null>(null);
	let connected = $state(false);
	let paused = $state(false);
	let lastUpdated = $state(0);
	let now = $state(performance.now());
	let error = $state<string | null>(null);

	let ws: ReconnectingWebSocket<Diagnostics> | null = null;
	let tick: ReturnType<typeof setInterval> | null = null;

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

	function leakedGoroutineValue(d: Diagnostics): string {
		if (d.runtime.leakScannedAt || d.runtime.leakedGoroutines > 0) {
			return fmtNum(d.runtime.leakedGoroutines);
		}
		return m.diagnostics_leaks_not_scanned();
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

	const wsCounts = $derived.by(() => {
		const s = diag?.websocket?.snapshot;
		if (!s) return [];
		return [
			{ label: m.diagnostics_ws_project_logs(), value: s.projectLogsActive },
			{ label: m.diagnostics_ws_container_logs(), value: s.containerLogsActive },
			{ label: m.diagnostics_ws_container_stats(), value: s.containerStats },
			{ label: m.diagnostics_ws_terminals(), value: s.containerExec },
			{ label: m.diagnostics_ws_system_stats(), value: s.systemStats },
			{ label: m.diagnostics_ws_service_logs(), value: s.serviceLogsActive }
		];
	});

	const diagnosticsTabItems = $derived.by(
		() =>
			[
				{ value: 'overview', label: m.common_overview(), icon: CpuIcon },
				{
					value: 'connections',
					label: m.diagnostics_tab_connections(),
					icon: ConnectionIcon,
					badge: totalConnections || undefined
				},
				{ value: 'logs', label: m.common_logs(), icon: ActivityIcon },
				{
					value: 'profiling',
					label: m.diagnostics_tab_profiling(),
					icon: DownloadIcon,
					badge: diag?.runtime.leakedGoroutines || undefined
				}
			] satisfies TabItem[]
	);

	const urlTab = useUrlTab<DiagnosticsTab>({
		validTabs: () => diagnosticsTabItems.map((tab) => tab.value as DiagnosticsTab),
		defaultTab: () => 'overview'
	});
	const activeTab = $derived(urlTab.value);

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

	const headerStats = $derived.by((): SettingsStatCard[] => {
		if (!diag) return [];
		return [
			{
				title: m.diagnostics_updated_ago({ ago: agoText }),
				value: paused ? m.paused() : connected ? m.common_live() : m.diagnostics_status_connecting(),
				icon: ActivityIcon,
				iconColor: paused ? 'text-amber-500' : connected ? 'text-emerald-500' : 'text-muted-foreground'
			},
			{
				title: m.goroutines(),
				value: fmtNum(diag.runtime.goroutines),
				icon: ActivityIcon,
				iconColor: diag.runtime.leakedGoroutines > 0 ? 'text-rose-500' : 'text-sky-500'
			},
			{
				title: m.diagnostics_stat_heap_alloc(),
				value: fmtBytes(diag.memory.heapAlloc),
				icon: MemoryStickIcon,
				iconColor: 'text-violet-500'
			},
			{
				title: m.common_uptime(),
				value: fmtUptime(diag.runtime.uptimeSeconds),
				icon: ClockIcon,
				iconColor: 'text-teal-500'
			}
		];
	});

	const actionButtons = $derived.by((): SettingsActionButton[] => [
		{
			id: 'pause',
			action: paused ? 'unpause' : 'pause',
			label: paused ? m.diagnostics_resume() : m.common_pause(),
			onclick: togglePause,
			showOnMobile: true
		},
		{
			id: 'refresh',
			action: 'refresh',
			label: m.common_refresh(),
			onclick: refresh,
			showOnMobile: true
		}
	]);

	function applySnapshot(d: Diagnostics) {
		diag = d;
		lastUpdated = performance.now();
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

	const dumps = [
		{ id: 'goroutine' as const, label: m.diagnostics_dump_goroutine() },
		{ id: 'heap' as const, label: m.diagnostics_dump_heap() }
	];

	const profiles: { id: PprofProfile; label: string }[] = [
		{ id: 'heap', label: m.diagnostics_profile_heap() },
		{ id: 'goroutine', label: m.diagnostics_profile_goroutine() },
		{ id: 'goroutineleak', label: m.diagnostics_profile_goroutineleak() },
		{ id: 'allocs', label: m.diagnostics_profile_allocs() },
		{ id: 'block', label: m.diagnostics_profile_block() },
		{ id: 'mutex', label: m.diagnostics_profile_mutex() },
		{ id: 'threadcreate', label: m.diagnostics_profile_threadcreate() },
		{ id: 'profile', label: m.diagnostics_profile_cpu() },
		{ id: 'trace', label: m.diagnostics_profile_trace() }
	];

	function onLeakScanned(report: GoroutineLeakReport) {
		if (!diag) return;
		diag.runtime.leakedGoroutines = report.count;
		diag.runtime.leakScannedAt = report.scannedAt;
	}

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
		tick = setInterval(() => (now = performance.now()), 1000);
		return () => {
			closeStream();
			if (tick) clearInterval(tick);
		};
	});
</script>

{#snippet statusList(rows: { label: string; value: string | number }[])}
	<dl class="grid grid-cols-[minmax(0,1fr)_auto] gap-x-6 gap-y-2.5 text-sm">
		{#each rows as r (r.label)}
			<dt class="text-muted-foreground">{r.label}</dt>
			<dd class="text-right font-medium tabular-nums">{r.value}</dd>
		{/each}
	</dl>
{/snippet}

{#snippet sectionHeading(title: string, description?: string)}
	<div class="space-y-1">
		<h3 class="text-base font-semibold">{title}</h3>
		{#if description}
			<p class="text-sm text-muted-foreground">{description}</p>
		{/if}
	</div>
{/snippet}

<SettingsPageLayout
	title={m.diagnostics()}
	description={m.diagnostics_description()}
	icon={ActivityIcon}
	pageType="management"
	statCards={headerStats}
	{actionButtons}
>
	{#snippet mainContent()}
		{#if error}
			<div class="mb-6 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
				{error}
			</div>
		{/if}

		{#if diag}
			<Tabs.Root value={activeTab} class="w-full">
				<TabBar items={diagnosticsTabItems} value={activeTab} onValueChange={urlTab.select} />

				<Tabs.Content value="overview" class="mt-6 space-y-8">
					{#if diag.runtime.leakedGoroutines > 0}
						<Alert.Root variant="destructive">
							<AlertTriangleIcon class="size-4" />
							<Alert.Title>{m.diagnostics_leaks_count({ count: diag.runtime.leakedGoroutines })}</Alert.Title>
							<Alert.Description class="flex flex-wrap items-center gap-3">
								{m.diagnostics_leaks_hint()}
								<ArcaneButton
									action="base"
									tone="outline"
									size="sm"
									customLabel={m.diagnostics_tab_profiling()}
									onclick={() => urlTab.select('profiling')}
								/>
							</Alert.Description>
						</Alert.Root>
					{/if}

					<section class="space-y-6">
						{@render sectionHeading(m.diagnostics_section_system())}
						<div class="grid gap-10 lg:grid-cols-2">
							<div class="space-y-3">
								<h4 class="text-xs font-semibold tracking-wider text-muted-foreground uppercase">
									{m.diagnostics_section_runtime()}
								</h4>
								{@render statusList([
									{ label: m.diagnostics_runtime_go_version(), value: diag.runtime.goVersion },
									{ label: m.platform(), value: `${diag.runtime.os}/${diag.runtime.arch}` },
									{ label: m.diagnostics_runtime_gomaxprocs(), value: diag.runtime.gomaxprocs },
									{ label: m.diagnostics_runtime_num_cpu(), value: diag.runtime.numCpu },
									{ label: m.goroutines(), value: fmtNum(diag.runtime.goroutines) },
									{ label: m.diagnostics_runtime_leaked_goroutines(), value: leakedGoroutineValue(diag) },
									{ label: m.diagnostics_runtime_ws_workers(), value: fmtNum(diag.runtime.wsWorkerGoroutines) },
									{ label: m.diagnostics_runtime_cgo_calls(), value: fmtNum(diag.runtime.numCgoCall) },
									{ label: m.common_uptime(), value: fmtUptime(diag.runtime.uptimeSeconds) }
								])}
							</div>
							<div class="space-y-3">
								<h4 class="text-xs font-semibold tracking-wider text-muted-foreground uppercase">
									{m.diagnostics_section_memory()}
								</h4>
								{#if heapBar}
									<div class="flex h-2 overflow-hidden rounded-full bg-muted/40">
										<div class="bg-violet-500" style="width: {heapBar.inuse}%" title={m.diagnostics_mem_in_use()}></div>
										<div class="bg-violet-500/40" style="width: {heapBar.idle}%" title={m.idle()}></div>
										<div class="bg-zinc-500/40" style="width: {heapBar.released}%" title={m.diagnostics_mem_released()}></div>
									</div>
									<div class="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
										<span>
											<span class="mr-1 inline-block size-2 rounded-full bg-violet-500"></span>
											{m.diagnostics_mem_in_use()}
											{fmtBytes(diag.memory.heapInuse)}
										</span>
										<span>
											<span class="mr-1 inline-block size-2 rounded-full bg-violet-500/40"></span>
											{m.idle()}
											{fmtBytes(diag.memory.heapIdle - diag.memory.heapReleased)}
										</span>
										<span>
											<span class="mr-1 inline-block size-2 rounded-full bg-zinc-500/40"></span>
											{m.diagnostics_mem_released()}
											{fmtBytes(diag.memory.heapReleased)}
										</span>
									</div>
								{/if}
								{@render statusList([
									{ label: m.diagnostics_mem_heap_alloc(), value: fmtBytes(diag.memory.heapAlloc) },
									{ label: m.diagnostics_mem_heap_sys(), value: fmtBytes(diag.memory.heapSys) },
									{ label: m.diagnostics_mem_heap_objects(), value: fmtNum(diag.memory.heapObjects) },
									{ label: m.diagnostics_mem_stack_in_use(), value: fmtBytes(diag.memory.stackInuse) },
									{ label: m.diagnostics_mem_total_alloc(), value: fmtBytes(diag.memory.totalAlloc) },
									{ label: m.diagnostics_mem_sys_total(), value: fmtBytes(diag.memory.sys) },
									{ label: m.diagnostics_mem_next_gc(), value: fmtBytes(diag.memory.nextGc) },
									{
										label: m.diagnostics_mem_gc_cpu_fraction(),
										value: `${(diag.memory.gcCpuFraction * 100).toFixed(3)}%`
									}
								])}
							</div>
						</div>
					</section>

					<section class="space-y-4 border-t border-border/50 pt-8">
						{@render sectionHeading(m.diagnostics_section_gc())}
						<div class="grid items-start gap-10 lg:grid-cols-2">
							{@render statusList([
								{ label: m.diagnostics_gc_total_cycles(), value: fmtNum(diag.gc.numGc) },
								{ label: m.diagnostics_gc_forced_cycles(), value: fmtNum(diag.memory.numForcedGc) },
								{ label: m.diagnostics_gc_total_pause(), value: fmtMs(diag.gc.pauseTotalNs) },
								{ label: m.diagnostics_gc_last(), value: diag.gc.lastGc ? formatTime(diag.gc.lastGc) : '—' }
							])}
							{#if diag.gc.recentPausesNs?.length}
								<div>
									<div class="mb-1 text-[11px] text-muted-foreground">{m.diagnostics_gc_recent_pauses()}</div>
									<div class="flex h-12 items-end gap-0.5">
										{#each diag.gc.recentPausesNs as p, i (i)}
											<div
												class="flex-1 rounded-sm bg-amber-500/70"
												style="height: {maxPause ? Math.max(4, (p / maxPause) * 100) : 4}%"
												title={fmtMs(p)}
											></div>
										{/each}
									</div>
								</div>
							{/if}
						</div>
					</section>
				</Tabs.Content>

				<Tabs.Content value="connections" class="mt-6 space-y-4">
					{@render sectionHeading(m.diagnostics_section_connections({ count: diag.websocket.connections?.length ?? 0 }))}
					<div class="grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-3 lg:grid-cols-6">
						{#each wsCounts as c (c.label)}
							<div>
								<div class="text-xs text-muted-foreground">{c.label}</div>
								<div class="text-lg font-semibold tabular-nums">{fmtNum(c.value)}</div>
							</div>
						{/each}
					</div>
					{#if diag.websocket.connections?.length}
						<div class="overflow-x-auto">
							<Table.Root>
								<Table.Header>
									<Table.Row>
										<Table.Head>{m.diagnostics_conn_kind()}</Table.Head>
										<Table.Head>{m.resource()}</Table.Head>
										<Table.Head>{m.diagnostics_conn_client_ip()}</Table.Head>
										<Table.Head>{m.common_user()}</Table.Head>
										<Table.Head>{m.diagnostics_conn_since()}</Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each diag.websocket.connections as c (c.id)}
										<Table.Row>
											<Table.Cell>{wsKindLabels[c.kind] ?? c.kind}</Table.Cell>
											<Table.Cell class="font-mono text-xs text-muted-foreground">{c.resourceId || '—'}</Table.Cell>
											<Table.Cell class="font-mono text-xs text-muted-foreground">{c.clientIp || '—'}</Table.Cell>
											<Table.Cell class="text-xs text-muted-foreground">{c.userId || '—'}</Table.Cell>
											<Table.Cell class="text-xs text-muted-foreground tabular-nums">
												{c.startedAt ? formatTime(c.startedAt) : '—'}
											</Table.Cell>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</div>
					{:else}
						<p class="py-8 text-sm text-muted-foreground">{m.diagnostics_connections_empty()}</p>
					{/if}
				</Tabs.Content>

				<Tabs.Content value="logs" class="mt-6">
					<DiagnosticLogPanel height="min(70vh, 640px)" />
				</Tabs.Content>

				<Tabs.Content value="profiling" class="mt-6 space-y-8">
					<section class="space-y-4">
						{@render sectionHeading(m.diagnostics_section_leaks())}
						<DiagnosticLeakPanel
							leakedGoroutines={diag.runtime.leakedGoroutines}
							leakScannedAt={diag.runtime.leakScannedAt}
							onscanned={onLeakScanned}
						/>
					</section>

					<section class="space-y-4 border-t border-border/50 pt-8">
						{@render sectionHeading(m.diagnostics_section_dumps())}
						<div class="space-y-2">
							{#each dumps as d (d.id)}
								<Collapsible.Root open={dumpOpen[d.id]} onOpenChange={(o: boolean) => onDumpToggle(d.id, o)}>
									<Collapsible.Trigger
										class="flex w-full items-center justify-between rounded-lg border border-border/60 px-3 py-2 text-sm font-medium hover:bg-muted/30"
									>
										{d.label}
										<ArrowDownIcon class={cn('size-4 transition-transform', dumpOpen[d.id] && '-rotate-180')} />
									</Collapsible.Trigger>
									<Collapsible.Content>
										<pre
											class="mt-1 max-h-96 overflow-auto rounded-lg border border-border/60 bg-background p-3 font-mono text-[11px] leading-relaxed">{dumpLoading[
												d.id
											]
												? m.common_loading()
												: dumpText[d.id] || m.diagnostics_dump_empty()}</pre>
									</Collapsible.Content>
								</Collapsible.Root>
							{/each}
						</div>
					</section>

					<section class="space-y-4 border-t border-border/50 pt-8">
						{@render sectionHeading(m.diagnostics_section_profiles(), m.diagnostics_profiles_hint())}
						<ul class="divide-y divide-border/40">
							{#each profiles as p (p.id)}
								<li class="flex items-center justify-between gap-4 py-2.5">
									<span class="text-sm font-medium">{p.label}</span>
									<ArcaneButton
										action="base"
										tone="outline"
										size="sm"
										icon={DownloadIcon}
										customLabel={m.diagnostics_download()}
										loading={downloading === p.id}
										disabled={downloading !== null}
										onclick={() => download(p.id)}
									/>
								</li>
							{/each}
						</ul>
					</section>
				</Tabs.Content>
			</Tabs.Root>
		{:else if !error}
			<div class="py-16 text-center text-sm text-muted-foreground">{m.diagnostics_loading()}</div>
		{/if}
	{/snippet}
</SettingsPageLayout>
