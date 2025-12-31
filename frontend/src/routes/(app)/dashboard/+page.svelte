<script lang="ts">
	import { toast } from 'svelte-sonner';
	import PruneConfirmationDialog from '$lib/components/dialogs/prune-confirmation-dialog.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { onMount } from 'svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { createStatsWebSocket } from '$lib/utils/ws';
	import type { ReconnectingWebSocket } from '$lib/utils/ws';
	import MeterMetric from '$lib/components/meter-metric.svelte';
	import DiskMeter from '$lib/components/disk-meter.svelte';
	import GpuMeter from '$lib/components/gpu-meter.svelte';
	import QuickActions from '$lib/components/quick-actions.svelte';
	import DockerOverview from '$lib/components/docker-overview.svelte';
	import type { SystemStats } from '$lib/types/system-stats.type';
	import DashboardContainerTable from './dash-container-table.svelte';
	import DashboardImageTable from './dash-image-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { invalidateAll } from '$app/navigation';
	import { systemService } from '$lib/services/system-service';
	import bytes from 'bytes';
	import { CpuIcon, MemoryStickIcon } from '$lib/icons';

	let { data } = $props();
	let containers = $derived(data.containers);
	let images = $derived(data.images);
	let dockerInfo = $derived(data.dockerInfo);
	let containerStatusCounts = $derived(data.containerStatusCounts);
	let settings = $derived(data.settings);

	let systemStats = $state<SystemStats | null>(null);
	let isPruneDialogOpen = $state(false);

	type PruneType = 'containers' | 'images' | 'networks' | 'volumes' | 'buildCache';

	let isLoading = $state({
		starting: false,
		stopping: false,
		refreshing: false,
		pruning: false,
		loadingStats: true,
		loadingDockerInfo: false,
		loadingContainers: false,
		loadingImages: false
	});

	let statsWSClient: ReconnectingWebSocket<SystemStats> | null = null;
	let hasInitialStatsLoaded = $state(false);

	let historicalData = $state({
		cpu: [] as Array<{ date: Date; value: number }>,
		memory: [] as Array<{ date: Date; value: number }>,
		disk: [] as Array<{ date: Date; value: number }>,
		gpu: [] as Array<{ date: Date; value: number }>,
		containers: [] as Array<{ date: Date; value: number }>
	});

	const stoppedContainers = $derived(containerStatusCounts.stoppedContainers);
	const runningContainers = $derived(containerStatusCounts.runningContainers);
	const totalContainers = $derived(containerStatusCounts.totalContainers);
	const currentStats = $derived(systemStats);

	function addToHistoricalData(stats: SystemStats) {
		const now = new Date();
		const maxPoints = 20;

		if (stats.cpuUsage !== undefined) {
			historicalData.cpu.push({ date: now, value: stats.cpuUsage });
			if (historicalData.cpu.length > maxPoints) {
				historicalData.cpu = historicalData.cpu.slice(-maxPoints);
			}
		}

		if (stats.memoryUsage !== undefined && stats.memoryTotal !== undefined) {
			const memoryPercent = (stats.memoryUsage / stats.memoryTotal) * 100;
			historicalData.memory.push({ date: now, value: memoryPercent });
			if (historicalData.memory.length > maxPoints) {
				historicalData.memory = historicalData.memory.slice(-maxPoints);
			}
		}

		if (stats.diskUsage !== undefined && stats.diskTotal !== undefined) {
			const diskPercent = (stats.diskUsage / stats.diskTotal) * 100;
			historicalData.disk.push({ date: now, value: diskPercent });
			if (historicalData.disk.length > maxPoints) {
				historicalData.disk = historicalData.disk.slice(-maxPoints);
			}
		}

		if (stats.gpus && stats.gpus.length > 0) {
			// Track average GPU memory usage percentage across all GPUs
			// Filter out GPUs with zero total memory to avoid invalid calculations
			const validGpus = stats.gpus.filter((gpu) => gpu.memoryTotal > 0);

			if (validGpus.length > 0) {
				const totalGpuPercent = validGpus.reduce((sum, gpu) => {
					return sum + (gpu.memoryUsed / gpu.memoryTotal) * 100;
				}, 0);
				const avgGpuPercent = totalGpuPercent / validGpus.length;
				historicalData.gpu.push({ date: now, value: avgGpuPercent });
				if (historicalData.gpu.length > maxPoints) {
					historicalData.gpu = historicalData.gpu.slice(-maxPoints);
				}
			}
		}

		historicalData.containers.push({ date: now, value: dockerInfo!.ContainersRunning });
		if (historicalData.containers.length > maxPoints) {
			historicalData.containers = historicalData.containers.slice(-maxPoints);
		}
	}

	async function refreshData() {
		isLoading.refreshing = true;
		await invalidateAll();
		isLoading.refreshing = false;
	}

	onMount(() => {
		let mounted = true;

		(async () => {
			await environmentStore.ready;

			if (mounted) {
				setupStatsWS();
			}
		})();

		return () => {
			mounted = false;
			statsWSClient?.close();
			statsWSClient = null;
		};
	});

	function resetStats() {
		systemStats = null;
		hasInitialStatsLoaded = false;
		historicalData = {
			cpu: [],
			memory: [],
			disk: [],
			gpu: [],
			containers: []
		};
	}

	function setupStatsWS() {
		if (statsWSClient) {
			statsWSClient.close();
			statsWSClient = null;
		}

		const getEnvId = () => {
			const env = environmentStore.selected;
			return env ? env.id : '0';
		};

		statsWSClient = createStatsWebSocket({
			getEnvId,
			onOpen: () => {
				if (!hasInitialStatsLoaded) {
					isLoading.loadingStats = true;
				}
			},
			onMessage: (data) => {
				systemStats = data;
				addToHistoricalData(data);
				hasInitialStatsLoaded = true;
				isLoading.loadingStats = false;
			},
			onError: (e) => {
				console.error('Stats websocket error:', e);
			}
		});
		statsWSClient.connect();
	}

	let lastEnvId: string | null = null;
	$effect(() => {
		const env = environmentStore.selected;
		if (!env) return;
		if (lastEnvId === null) {
			lastEnvId = env.id;
			return;
		}
		if (env.id !== lastEnvId) {
			lastEnvId = env.id;
			statsWSClient?.close();
			statsWSClient = null;
			resetStats();
			setupStatsWS();
			refreshData();
		}
	});

	async function handleStartAll() {
		if (isLoading.starting || !dockerInfo || stoppedContainers === 0) return;
		isLoading.starting = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(systemService.startAllStoppedContainers()),
			message: m.dashboard_start_all_failed(),
			setLoadingState: (value) => (isLoading.starting = value),
			onSuccess: async () => {
				toast.success(m.dashboard_start_all_success());
				await refreshData();
			}
		});
	}

	async function handleStopAll() {
		if (isLoading.stopping || !dockerInfo || dockerInfo?.ContainersRunning === 0) return;
		openConfirmDialog({
			title: m.dashboard_stop_all_title(),
			message: m.dashboard_stop_all_confirm(),
			confirm: {
				label: m.common_confirm(),
				destructive: false,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(systemService.stopAllContainers()),
						message: m.dashboard_stop_all_failed(),
						setLoadingState: (value) => (isLoading.stopping = value),
						onSuccess: async () => {
							toast.success(m.dashboard_stop_all_success());
							await refreshData();
						}
					});
				}
			}
		});
	}

	async function confirmPrune(selectedTypes: PruneType[]) {
		if (isLoading.pruning || selectedTypes.length === 0) return;
		isLoading.pruning = true;

		const pruneOptions = {
			containers: selectedTypes.includes('containers'),
			images: selectedTypes.includes('images'),
			volumes: selectedTypes.includes('volumes'),
			networks: selectedTypes.includes('networks'),
			buildCache: selectedTypes.includes('buildCache'),
			dangling: settings?.dockerPruneMode === 'dangling'
		};

		const typeLabels: Record<PruneType, string> = {
			containers: m.prune_stopped_containers(),
			images: m.prune_unused_images(),
			networks: m.prune_unused_networks(),
			volumes: m.prune_unused_volumes(),
			buildCache: m.build_cache()
		};
		const typesString = selectedTypes.map((t) => typeLabels[t]).join(', ');

		handleApiResultWithCallbacks({
			result: await tryCatch(systemService.pruneAll(pruneOptions)),
			message: m.dashboard_prune_failed({ types: typesString }),
			setLoadingState: (value) => (isLoading.pruning = value),
			onSuccess: async () => {
				isPruneDialogOpen = false;
				if (selectedTypes.length === 1) {
					toast.success(m.dashboard_prune_success_one({ types: typesString }));
				} else {
					toast.success(m.dashboard_prune_success_many({ types: typesString }));
				}
				await refreshData();
			}
		});
	}
</script>

<div class="flex min-h-full flex-col space-y-6 pt-3 md:space-y-10 md:pt-5">
	<header class="flex items-center justify-between gap-4">
		<div class="flex min-w-0 flex-1 items-center gap-3 sm:gap-4">
			<div class="min-w-0">
				<h1 class="text-2xl font-bold tracking-tight sm:text-3xl">{m.dashboard_title()}</h1>
				<p class="text-muted-foreground mt-1 text-sm">{m.dashboard_subtitle()}</p>
			</div>
		</div>

		<QuickActions
			class="min-w-0 flex-1"
			compact
			{dockerInfo}
			{stoppedContainers}
			{runningContainers}
			loadingDockerInfo={isLoading.loadingDockerInfo}
			isLoading={{ starting: isLoading.starting, stopping: isLoading.stopping, pruning: isLoading.pruning }}
			onStartAll={handleStartAll}
			onStopAll={handleStopAll}
			onOpenPruneDialog={() => (isPruneDialogOpen = true)}
			onRefresh={refreshData}
			refreshing={isLoading.refreshing}
		/>
	</header>

	<section>
		<div class="mb-4 flex items-center justify-between gap-4">
			<h2 class="text-lg font-semibold tracking-tight">{m.dashboard_system_overview()}</h2>
			{#if currentStats?.hostname}
				<div class="text-muted-foreground flex items-center gap-2 text-sm">
					<span class="text-muted-foreground/70">Hostname:</span>
					<code class="bg-muted rounded px-2 py-0.5 font-mono text-xs">{currentStats.hostname}</code>
				</div>
			{/if}
		</div>
		<div class="space-y-3">
			<div
				class="grid grid-cols-2 gap-3 sm:grid-cols-2 {currentStats?.gpuCount && currentStats.gpuCount > 0
					? 'lg:grid-cols-4'
					: 'lg:grid-cols-3'}"
			>
				<MeterMetric
					title={m.dashboard_meter_cpu()}
					icon={CpuIcon}
					description={m.dashboard_meter_cpu_desc()}
					currentValue={isLoading.loadingStats || !hasInitialStatsLoaded ? undefined : currentStats?.cpuUsage}
					unit="%"
					maxValue={100}
					formatValue={(v) => `${v.toFixed(1)}`}
					loading={isLoading.loadingStats || !hasInitialStatsLoaded}
					showAbsoluteValues={true}
					formatAbsoluteValue={() => `${currentStats?.cpuCount || 0} ${m.common_cpus()}`}
				/>

				<MeterMetric
					title={m.dashboard_meter_memory()}
					icon={MemoryStickIcon}
					description={m.dashboard_meter_memory_desc()}
					currentValue={isLoading.loadingStats || !hasInitialStatsLoaded ? undefined : currentStats?.memoryUsage}
					unit="%"
					formatValue={(v) => {
						if (currentStats?.memoryTotal) {
							return ((v / currentStats.memoryTotal) * 100).toFixed(1);
						}
						return '0';
					}}
					maxValue={currentStats?.memoryTotal}
					showAbsoluteValues={true}
					formatAbsoluteValue={(v) => bytes.format(v, { unitSeparator: ' ' }) ?? '-'}
					loading={isLoading.loadingStats || !hasInitialStatsLoaded}
				/>

				<DiskMeter
					diskUsage={currentStats?.diskUsage}
					diskTotal={currentStats?.diskTotal}
					loading={isLoading.loadingStats || !hasInitialStatsLoaded}
					class="col-span-2 sm:col-span-1"
				/>

				{#if currentStats?.gpuCount && currentStats.gpuCount > 0}
					<GpuMeter gpus={currentStats?.gpus} loading={isLoading.loadingStats || !hasInitialStatsLoaded} />
				{/if}
			</div>

			<DockerOverview
				{dockerInfo}
				containersRunning={runningContainers}
				containersStopped={stoppedContainers}
				{totalContainers}
				totalImages={images.pagination.totalItems}
				loading={isLoading.loadingDockerInfo}
			/>
		</div>
	</section>

	<section class="flex min-h-0 flex-1 flex-col">
		<h2 class="mb-4 text-lg font-semibold tracking-tight">Resources</h2>
		<div class="grid min-h-0 flex-1 grid-cols-1 gap-6 lg:grid-cols-2">
			<DashboardContainerTable bind:containers isLoading={isLoading.loadingContainers} />
			<DashboardImageTable bind:images isLoading={isLoading.loadingImages} />
		</div>
	</section>

	<PruneConfirmationDialog
		bind:open={isPruneDialogOpen}
		isPruning={isLoading.pruning}
		imagePruneMode={(settings?.dockerPruneMode as 'dangling' | 'all') || 'dangling'}
		onConfirm={confirmPrune}
		onCancel={() => (isPruneDialogOpen = false)}
	/>
</div>
