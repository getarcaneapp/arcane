<script lang="ts">
	import { toast } from 'svelte-sonner';

	import LogControls from '#lib/components/logs/log-controls.svelte';
	import LogViewer from '#lib/components/logs/log-viewer.svelte';
	// Dozzle reference: the compact CPU/memory monitors in the logs header were informed
	// by amir20/dozzle's ContainerLog.vue and MultiContainerStat.vue.
	import { UseLogPreferences } from '#lib/hooks/use-log-preferences.svelte.js';
	import { CpuIcon, MemoryStickIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerService } from '#lib/services/container-service.js';
	import type { ContainerStats, ContainerStatsHistorySample } from '#lib/types/container.js';
	import { calculateCPUPercent, calculateMemoryUsage } from '#lib/utils/docker.js';
	import { bytes } from '#lib/utils/formatting.js';
	import { refreshLogViewerStream, startLogViewerStream, stopLogViewerStream } from '#lib/utils/log-viewer.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import ContainerLogStatMonitor from './container-log-stat-monitor.svelte';

	type StatHistoryPoint = { percent: number; tooltip: string };

	let {
		containerId,
		environmentId,
		stats = null,
		hasInitialStatsLoaded = false,
		isRunning = false,
		cpuLimit = 0,
		autoScroll = $bindable(),
		onStart,
		onStop
	}: {
		containerId: string | undefined;
		environmentId: string;
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

	async function handleDownload() {
		if (!containerId) return;
		const result = await tryCatch(containerService.downloadContainerLogs(containerId, environmentId));
		if (result.error !== null) {
			toast.error(m.common_download_error(), { description: result.error.message });
		}
	}

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

<div class="flex h-full min-h-0 flex-col gap-3">
	<div class="grid shrink-0 gap-3 md:grid-cols-2" data-testid="container-log-stats">
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
	<div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl bg-table-head">
		<div class="flex h-11 shrink-0 items-center gap-2 pr-1.5 pl-3">
			<h2 class="text-sm font-medium">{m.common_logs()}</h2>
			<div class="ml-auto min-w-0">
				<LogControls
					bind:searchTerm={logSearchTerm}
					bind:autoScroll
					{preferences}
					{isStreaming}
					disabled={!containerId}
					onStart={handleStart}
					onStop={handleStop}
					onRefresh={handleRefresh}
					onDownload={handleDownload}
				/>
			</div>
		</div>
		<LogViewer
			class="min-h-0 flex-1"
			searchTerm={logSearchTerm}
			bind:this={viewer}
			bind:autoScroll
			type="container"
			{containerId}
			{environmentId}
			bind:showParsedJson={preferences.showParsedJson}
			showStreamLabels={preferences.showStreamLabels}
			tailLines={preferences.tailLines}
			maxLines={500}
			showTimestamps={preferences.showTimestamps}
			groupAdjacentLines={true}
			height="100%"
			onStart={handleStreamStart}
			onStop={handleStreamStop}
		/>
	</div>
</div>
