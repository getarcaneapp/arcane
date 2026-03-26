<script lang="ts">
	// Dozzle reference: the compact CPU/memory monitors in the logs header were informed
	// by amir20/dozzle's ContainerLog.vue and MultiContainerStat.vue.
	import * as Card from '$lib/components/ui/card';
	import LogViewer from '$lib/components/logs/log-viewer.svelte';
	import LogControls from '$lib/components/logs/log-controls.svelte';
	import type { ContainerStats, ContainerStatsHistorySample } from '$lib/types/container.type';
	import { m } from '$lib/paraglide/messages';
	import bytes from '$lib/utils/bytes';
	import { calculateCPUPercent, calculateMemoryUsage } from '$lib/utils/container-stats.utils';
	import { CpuIcon, FileTextIcon, MemoryStickIcon } from '$lib/icons';
	import ContainerLogStatMonitor from './ContainerLogStatMonitor.svelte';

	const HISTORY_LIMIT = 30;
	type StatHistoryPoint = { percent: number; tooltip: string };

	let {
		containerId,
		stats = null,
		hasInitialStatsLoaded = false,
		isRunning = false,
		cpuLimit = 0,
		autoScroll = $bindable(),
		onStart,
		onStop
	}: {
		containerId: string | undefined;
		stats?: ContainerStats | null;
		hasInitialStatsLoaded?: boolean;
		isRunning?: boolean;
		cpuLimit?: number;
		autoScroll: boolean;
		onStart?: () => void;
		onStop?: () => void;
	} = $props();

	let isStreaming = $state(false);
	let viewer = $state<LogViewer>();
	let autoStartLogs = $state(false);
	let hasAutoStarted = $state(false);
	let showParsedJson = $state(false);
	let cpuHistory = $state<StatHistoryPoint[]>([]);
	let memoryHistory = $state<StatHistoryPoint[]>([]);
	let cpuHistoryAccumulator: StatHistoryPoint[] = [];
	let memoryHistoryAccumulator: StatHistoryPoint[] = [];
	let lastStatsRead: string | null = null;

	const cpuUsagePercent = $derived(calculateCPUPercent(stats));
	const memoryUsageBytes = $derived(calculateMemoryUsage(stats));
	const memoryLimitBytes = $derived(stats?.memory_stats?.limit || 0);
	const memoryUsagePercent = $derived.by(() => {
		if (!memoryLimitBytes) return 0;
		return Math.min(Math.max((memoryUsageBytes / memoryLimitBytes) * 100, 0), 100);
	});
	const cpuValue = $derived(isRunning ? `${cpuUsagePercent.toFixed(1)}%` : m.common_na());
	const cpuDetail = $derived.by(() => {
		if (!isRunning) return m.common_na();
		if (cpuLimit > 0) {
			const rounded = Number.isInteger(cpuLimit) ? cpuLimit.toFixed(0) : cpuLimit.toFixed(1);
			return `${rounded} ${cpuLimit === 1 ? m.containers_stats_cpu_unit_singular() : m.common_cpus()}`;
		}
		const onlineCpus = stats?.cpu_stats?.online_cpus ?? 0;
		return onlineCpus > 0
			? `${onlineCpus} ${onlineCpus === 1 ? m.containers_stats_cpu_unit_singular() : m.common_cpus()}`
			: m.common_na();
	});
	const memoryValue = $derived(isRunning ? bytes.format(memoryUsageBytes, { unitSeparator: ' ' }) : m.common_na());
	const memoryDetail = $derived.by(() => {
		if (!isRunning) return m.common_na();
		if (!memoryLimitBytes) return m.common_na();
		return bytes.format(memoryLimitBytes, { unitSeparator: ' ' });
	});

	function resetHistory() {
		cpuHistoryAccumulator = [];
		memoryHistoryAccumulator = [];
		cpuHistory = [];
		memoryHistory = [];
		lastStatsRead = null;
	}

	function appendHistorySample(history: StatHistoryPoint[], value: StatHistoryPoint): StatHistoryPoint[] {
		const next = [...history, value];
		if (next.length > HISTORY_LIMIT) {
			next.shift();
		}
		return next;
	}

	function percentFromTenths(tenths: number | undefined): number {
		if (typeof tenths !== 'number' || Number.isNaN(tenths)) {
			return 0;
		}

		return Math.min(Math.max(tenths / 10, 0), 100);
	}

	function formatPercent(value: number, digits = 2): string {
		if (value > 0 && value < 0.01) {
			return '<0.01%';
		}

		return `${value.toFixed(digits)}%`;
	}

	function toCPUHistoryPoint(sample: ContainerStatsHistorySample): StatHistoryPoint {
		const percent = percentFromTenths(sample.cpuTenths);
		return {
			percent,
			tooltip: formatPercent(percent, 1)
		};
	}

	function toMemoryHistoryPoint(sample: ContainerStatsHistorySample): StatHistoryPoint {
		const percent = percentFromTenths(sample.memoryTenths);
		const usage = bytes.format(sample.memoryUsageBytes || 0, { unitSeparator: ' ' });
		const limit = memoryLimitBytes ? bytes.format(memoryLimitBytes, { unitSeparator: ' ' }) : '';

		return {
			percent,
			tooltip: limit ? `${usage} / ${limit} (${formatPercent(percent, 2)})` : `${usage} (${formatPercent(percent, 2)})`
		};
	}

	function applyBackendHistory(samples: ContainerStatsHistorySample[] | undefined) {
		const history = samples ?? [];
		cpuHistoryAccumulator = history.map(toCPUHistoryPoint);
		memoryHistoryAccumulator = history.map(toMemoryHistoryPoint);
		cpuHistory = cpuHistoryAccumulator;
		memoryHistory = memoryHistoryAccumulator;
	}

	function handleStart() {
		viewer?.startLogStream();
	}

	function handleStop() {
		viewer?.stopLogStream();
	}

	async function handleRefresh() {
		await viewer?.clearLogs({ hard: true, restart: true });
	}

	// Sync isStreaming from viewer callbacks
	function handleStreamStart() {
		isStreaming = true;
		onStart?.();
	}

	function handleStreamStop() {
		isStreaming = false;
		onStop?.();
	}

	$effect(() => {
		if (containerId) {
			hasAutoStarted = false;
			resetHistory();
		}
	});

	$effect(() => {
		if (autoStartLogs && !hasAutoStarted && !isStreaming && containerId) {
			hasAutoStarted = true;
			handleStart();
		}
	});

	$effect(() => {
		if (!isRunning) {
			return;
		}

		if (stats?.statsHistory?.length) {
			applyBackendHistory(stats.statsHistory);
			lastStatsRead = stats.read;
			return;
		}

		if (!stats?.read || stats.read === lastStatsRead) {
			return;
		}

		lastStatsRead = stats.read;
		if (stats.currentHistorySample) {
			cpuHistoryAccumulator = appendHistorySample(cpuHistoryAccumulator, toCPUHistoryPoint(stats.currentHistorySample));
			memoryHistoryAccumulator = appendHistorySample(memoryHistoryAccumulator, toMemoryHistoryPoint(stats.currentHistorySample));
			cpuHistory = cpuHistoryAccumulator;
			memoryHistory = memoryHistoryAccumulator;
		}
	});
</script>

<Card.Root class="flex h-full min-h-0 flex-col">
	<Card.Header icon={FileTextIcon}>
		<div class="flex flex-1 flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
			<div class="flex flex-col gap-1.5">
				<div class="flex items-center gap-2">
					<Card.Title>
						<h2>
							{m.containers_logs_title()}
						</h2>
					</Card.Title>
					{#if isStreaming}
						<div class="flex items-center gap-2">
							<div class="size-2 animate-pulse rounded-full bg-green-500"></div>
							<span class="text-xs font-semibold text-green-600 sm:text-sm">{m.common_live()}</span>
						</div>
					{/if}
				</div>
				<Card.Description>{m.containers_logs_description()}</Card.Description>
			</div>
			<LogControls
				bind:autoScroll
				bind:autoStartLogs
				bind:showParsedJson
				{isStreaming}
				disabled={!containerId}
				onStart={handleStart}
				onStop={handleStop}
				onRefresh={handleRefresh}
			/>
		</div>
	</Card.Header>
	<Card.Content class="flex min-h-0 flex-1 flex-col p-0">
		<div class="shrink-0 border-b px-4 pb-4" data-testid="container-log-stats">
			<div class="grid gap-3 md:grid-cols-2">
				<ContainerLogStatMonitor
					icon={CpuIcon}
					label={m.dashboard_meter_cpu()}
					value={cpuValue}
					detail={cpuDetail}
					history={cpuHistory}
					tone="cpu"
					loading={isRunning && !hasInitialStatsLoaded}
					disabled={!isRunning}
					testId="container-log-cpu-monitor"
				/>
				<ContainerLogStatMonitor
					icon={MemoryStickIcon}
					label={m.dashboard_meter_memory()}
					value={memoryValue}
					detail={memoryDetail}
					history={memoryHistory}
					tone="memory"
					loading={isRunning && !hasInitialStatsLoaded}
					disabled={!isRunning}
					testId="container-log-memory-monitor"
				/>
			</div>
		</div>
		<div class="bg-card/90 min-h-0 flex-1 overflow-hidden rounded-lg border p-0 backdrop-blur-sm">
			<LogViewer
				bind:this={viewer}
				bind:autoScroll
				type="container"
				{containerId}
				{showParsedJson}
				maxLines={500}
				showTimestamps={true}
				groupAdjacentLines={true}
				height="100%"
				onStart={handleStreamStart}
				onStop={handleStreamStop}
			/>
		</div>
	</Card.Content>
</Card.Root>
