<script lang="ts">
	// Dozzle reference: the compact CPU/memory monitors in the logs header were informed
	// by amir20/dozzle's ContainerLog.vue and MultiContainerStat.vue.
	import * as Card from '#lib/components/ui/card/index.js';
	import LogViewer from '#lib/components/logs/log-viewer.svelte';
	import LogControls from '#lib/components/logs/log-controls.svelte';
	import { UseLogPreferences } from '#lib/hooks/use-log-preferences.svelte.js';
	import LogPanelTitle from '#lib/components/logs/log-panel-title.svelte';
	import type { ContainerStats, ContainerStatsHistorySample } from '#lib/types/docker.js';
	import { m } from '#lib/paraglide/messages.js';
	import { bytes } from '#lib/utils/formatting.js';
	import { calculateCPUPercent, calculateMemoryUsage } from '#lib/utils/docker.js';
	import { refreshLogViewerStream, startLogViewerStream, stopLogViewerStream } from '#lib/utils/log-viewer.js';
	import { CpuIcon, FileTextIcon, MemoryStickIcon } from '#lib/icons/index.js';
	import ContainerLogStatMonitor from './ContainerLogStatMonitor.svelte';

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
	let viewer = $state<ReturnType<typeof LogViewer>>();
	const preferences = new UseLogPreferences();
	let logSearchTerm = $state('');
	let hasAutoStarted = $state(false);
	const cpuHistory = $derived((stats?.statsHistory ?? []).map(toCPUHistoryPoint));
	const memoryHistory = $derived((stats?.statsHistory ?? []).map(toMemoryHistoryPoint));

	const cpuUsagePercent = $derived(calculateCPUPercent(stats));
	const memoryUsageBytes = $derived(calculateMemoryUsage(stats));
	const memoryLimitBytes = $derived(stats?.memory_stats?.limit || 0);
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
	const memoryValue = $derived(isRunning ? (bytes.format(memoryUsageBytes, { unitSeparator: ' ' }) ?? '') : m.common_na());
	const memoryDetail = $derived.by(() => {
		if (!isRunning) return m.common_na();
		if (!memoryLimitBytes) return m.common_na();
		return bytes.format(memoryLimitBytes, { unitSeparator: ' ' }) ?? '';
	});

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

	function handleStart() {
		startLogViewerStream(viewer);
	}

	function handleStop() {
		stopLogViewerStream(viewer);
	}

	async function handleRefresh() {
		await refreshLogViewerStream(viewer);
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
		if (preferences.autoStartLogs && !hasAutoStarted && !isStreaming && containerId && viewer) {
			hasAutoStarted = true;
			handleStart();
		}
	});
</script>

<Card.Root class="flex h-full min-h-0 flex-col">
	<Card.Header icon={FileTextIcon}>
		<div class="flex flex-1 flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
			<div class="flex flex-col gap-1.5">
				<div class="flex items-start justify-between gap-3 lg:block">
					<LogPanelTitle title={m.containers_logs_title()} live={isStreaming} />
					<LogControls
						bind:searchTerm={logSearchTerm}
						bind:autoScroll
						{preferences}
						mobileLayout="full"
						showDesktop={false}
						{isStreaming}
						disabled={!containerId}
						onStart={handleStart}
						onStop={handleStop}
						onRefresh={handleRefresh}
					/>
				</div>
				<Card.Description>{m.containers_logs_description()}</Card.Description>
			</div>
			<LogControls
				bind:searchTerm={logSearchTerm}
				bind:autoScroll
				{preferences}
				mobileLayout="none"
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
					label={m.cpu_usage()}
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
					label={m.memory_usage()}
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
		<div class="min-h-0 flex-1 overflow-hidden rounded-lg border bg-card/90 p-0 backdrop-blur-sm">
			<LogViewer
				searchTerm={logSearchTerm}
				bind:this={viewer}
				bind:autoScroll
				type="container"
				{containerId}
				bind:showParsedJson={preferences.showParsedJson}
				tailLines={preferences.tailLines}
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
