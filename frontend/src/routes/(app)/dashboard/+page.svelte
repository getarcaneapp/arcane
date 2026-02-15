<script lang="ts">
	import { toast } from 'svelte-sonner';
	import PruneConfirmationDialog from '$lib/components/dialogs/prune-confirmation-dialog.svelte';
	import DockerInfoDialog from '$lib/components/dialogs/docker-info-dialog.svelte';
	import * as Card from '$lib/components/ui/card/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { onMount } from 'svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { createStatsWebSocket } from '$lib/utils/ws';
	import type { ReconnectingWebSocket } from '$lib/utils/ws';
	import QuickActions from '$lib/components/quick-actions.svelte';
	import type { SystemStats } from '$lib/types/system-stats.type';
	import DashboardContainerTable from './dash-container-table.svelte';
	import DashboardImageTable from './dash-image-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { invalidateAll } from '$app/navigation';
	import { systemService } from '$lib/services/system-service';
	import bytes from '$lib/utils/bytes';
	import {
		CpuIcon,
		MemoryStickIcon,
		StatsIcon,
		UpdateIcon,
		AlertTriangleIcon,
		ShieldCheckIcon,
		VolumesIcon,
		ArrowRightIcon,
		ContainersIcon,
		ImagesIcon,
		GpuIcon,
		InfoIcon
	} from '$lib/icons';

	let { data } = $props();
	let containers = $derived(data.containers);
	let images = $derived(data.images);
	let dockerInfo = $derived(data.dockerInfo);
	let containerStatusCounts = $derived(data.containerStatusCounts);
	let settings = $derived(data.settings);
	let imageUsageCounts = $derived(data.imageUsageCounts);
	let vulnerabilitySummary = $derived(data.vulnerabilitySummary);

	let systemStats = $state<SystemStats | null>(null);
	let isPruneDialogOpen = $state(false);
	let dockerInfoDialogOpen = $state(false);

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

	const stoppedContainers = $derived(containerStatusCounts.stoppedContainers);
	const runningContainers = $derived(containerStatusCounts.runningContainers);
	const totalContainers = $derived(containerStatusCounts.totalContainers);
	const currentStats = $derived(systemStats);

	const updatesAvailableCount = $derived((images?.data ?? []).filter((image) => image.updateInfo?.hasUpdate).length);
	const securitySummary = $derived(vulnerabilitySummary?.summary);
	const criticalVulnerabilities = $derived(securitySummary?.critical ?? 0);
	const highVulnerabilities = $derived(securitySummary?.high ?? 0);
	const totalVulnerabilities = $derived(securitySummary?.total ?? 0);
	const actionableVulnerabilities = $derived(criticalVulnerabilities + highVulnerabilities);

	const imagesInUseCount = $derived(imageUsageCounts?.imagesInuse ?? 0);
	const imagesUnusedCount = $derived(imageUsageCounts?.imagesUnused ?? 0);

	const containerHealthPercent = $derived(totalContainers > 0 ? Math.round((runningContainers / totalContainers) * 100) : 100);
	const diskUsagePercent = $derived.by(() => {
		if (!currentStats?.diskTotal || currentStats.diskTotal <= 0 || currentStats.diskUsage === undefined) return null;
		return (currentStats.diskUsage / currentStats.diskTotal) * 100;
	});
	const diskRisk = $derived.by(() => {
		if (diskUsagePercent === null) return 'unknown';
		if (diskUsagePercent >= 90) return 'critical';
		if (diskUsagePercent >= 80) return 'high';
		if (diskUsagePercent >= 65) return 'moderate';
		return 'healthy';
	});

	const cpuMetric = $derived.by(() => {
		if (isLoading.loadingStats || !hasInitialStatsLoaded) return null;
		return currentStats?.cpuUsage ?? null;
	});
	const memoryMetric = $derived.by(() => {
		if (isLoading.loadingStats || !hasInitialStatsLoaded) return null;
		if (currentStats?.memoryUsage === undefined || !currentStats.memoryTotal) return null;
		return (currentStats.memoryUsage / currentStats.memoryTotal) * 100;
	});
	const diskMetric = $derived.by(() => {
		if (isLoading.loadingStats || !hasInitialStatsLoaded) return null;
		return diskUsagePercent;
	});
	const gpuMetric = $derived.by(() => {
		if (isLoading.loadingStats || !hasInitialStatsLoaded) return null;
		const gpus = currentStats?.gpus?.filter((gpu) => gpu.memoryTotal > 0) ?? [];
		if (gpus.length === 0) return null;
		const totalPercent = gpus.reduce((sum, gpu) => sum + (gpu.memoryUsed / gpu.memoryTotal) * 100, 0);
		return totalPercent / gpus.length;
	});

	const cpuMetricLabel = $derived.by(() => `${currentStats?.cpuCount ?? 0} ${m.common_cpus()}`);
	const memoryMetricLabel = $derived.by(() => {
		if (currentStats?.memoryUsage === undefined || !currentStats.memoryTotal) return '--';
		return `${bytes.format(currentStats.memoryUsage, { unitSeparator: ' ' }) ?? '-'} / ${bytes.format(currentStats.memoryTotal, { unitSeparator: ' ' }) ?? '-'}`;
	});
	const diskMetricLabel = $derived.by(() => {
		if (currentStats?.diskUsage === undefined || !currentStats.diskTotal) return '--';
		return `${bytes.format(currentStats.diskUsage, { unitSeparator: ' ' }) ?? '-'} / ${bytes.format(currentStats.diskTotal, { unitSeparator: ' ' }) ?? '-'}`;
	});
	const gpuMetricLabel = $derived.by(() => {
		const count = currentStats?.gpuCount ?? 0;
		return count > 0 ? `${count} GPU${count === 1 ? '' : 's'}` : '--';
	});

	function formatPercent(value: number | null): string {
		return value === null ? '--' : `${value.toFixed(1)}%`;
	}

	function meterWidth(value: number | null): string {
		const safe = value === null ? 0 : Math.max(0, Math.min(100, value));
		return `width: ${safe}%`;
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

<div class="flex min-h-full flex-col gap-4 pt-3 md:gap-5 md:pt-4">
	<header class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
		<div class="space-y-1">
			<h1 class="text-2xl font-bold tracking-tight sm:text-3xl">{m.dashboard_title()}</h1>
			<p class="text-muted-foreground text-sm">{m.dashboard_subtitle()}</p>
		</div>

		<QuickActions
			class="w-full justify-start lg:w-auto lg:justify-end"
			compact
			user={data.user}
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

	<section class="space-y-1.5">
		<div class="flex flex-col gap-1">
			<h2 class="text-lg font-semibold tracking-tight">Needs attention</h2>
			<p class="text-muted-foreground text-sm">Focus on the highest-impact maintenance tasks first.</p>
		</div>

		<div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
			<Card.Root>
				<Card.Content class="space-y-2 p-4">
					<div class="flex items-center justify-between gap-2">
						<div class="text-muted-foreground flex items-center gap-2 text-sm font-medium">
							<ContainersIcon class="size-4" />
							Containers
						</div>
						{#if stoppedContainers > 0}
							<Badge variant="destructive">{stoppedContainers} stopped</Badge>
						{:else}
							<Badge variant="secondary">Healthy</Badge>
						{/if}
					</div>
					<p class="text-xl font-semibold">{runningContainers}/{totalContainers}</p>
					<p class="text-muted-foreground text-xs">Running containers</p>
					<ArcaneButton action="base" tone="ghost" size="sm" href="/containers" class="w-full justify-between">
						Review containers
						<ArrowRightIcon class="size-4" />
					</ArcaneButton>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Content class="space-y-2 p-4">
					<div class="flex items-center justify-between gap-2">
						<div class="text-muted-foreground flex items-center gap-2 text-sm font-medium">
							<UpdateIcon class="size-4" />
							Image updates
						</div>
						{#if updatesAvailableCount > 0}
							<Badge variant="destructive">{updatesAvailableCount} pending</Badge>
						{:else}
							<Badge variant="secondary">Up to date</Badge>
						{/if}
					</div>
					<p class="text-xl font-semibold">{updatesAvailableCount}</p>
					<p class="text-muted-foreground text-xs">Updates found in the current image snapshot</p>
					<ArcaneButton action="base" tone="ghost" size="sm" href="/images" class="w-full justify-between">
						Review images
						<ArrowRightIcon class="size-4" />
					</ArcaneButton>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Content class="space-y-2 p-4">
					<div class="flex items-center justify-between gap-2">
						<div class="text-muted-foreground flex items-center gap-2 text-sm font-medium">
							{#if actionableVulnerabilities > 0}
								<AlertTriangleIcon class="size-4" />
							{:else}
								<ShieldCheckIcon class="size-4" />
							{/if}
							Security
						</div>
						{#if actionableVulnerabilities > 0}
							<Badge variant="destructive">{actionableVulnerabilities} actionable</Badge>
						{:else if totalVulnerabilities > 0}
							<Badge variant="outline">{totalVulnerabilities} tracked</Badge>
						{:else}
							<Badge variant="secondary">No findings</Badge>
						{/if}
					</div>
					<p class="text-xl font-semibold">{criticalVulnerabilities + highVulnerabilities}</p>
					<p class="text-muted-foreground text-xs">Critical + high vulnerabilities</p>
					<ArcaneButton action="base" tone="ghost" size="sm" href="/security" class="w-full justify-between">
						Open security
						<ArrowRightIcon class="size-4" />
					</ArcaneButton>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Content class="space-y-2 p-4">
					<div class="flex items-center justify-between gap-2">
						<div class="text-muted-foreground flex items-center gap-2 text-sm font-medium">
							<VolumesIcon class="size-4" />
							Disk pressure
						</div>
						{#if diskRisk === 'critical' || diskRisk === 'high'}
							<Badge variant="destructive">{diskRisk}</Badge>
						{:else if diskRisk === 'moderate'}
							<Badge variant="outline">moderate</Badge>
						{:else if diskRisk === 'healthy'}
							<Badge variant="secondary">healthy</Badge>
						{:else}
							<Badge variant="outline">unknown</Badge>
						{/if}
					</div>
					<p class="text-xl font-semibold">{diskUsagePercent === null ? '--' : `${diskUsagePercent.toFixed(1)}%`}</p>
					<p class="text-muted-foreground text-xs">
						{#if currentStats?.diskUsage !== undefined && currentStats?.diskTotal}
							{bytes.format(currentStats.diskUsage, { unitSeparator: ' ' }) ?? '-'} / {bytes.format(currentStats.diskTotal, {
								unitSeparator: ' '
							}) ?? '-'}
						{:else}
							Waiting for live disk stats
						{/if}
					</p>
					<ArcaneButton action="base" tone="ghost" size="sm" href="/volumes" class="w-full justify-between">
						Manage volumes
						<ArrowRightIcon class="size-4" />
					</ArcaneButton>
				</Card.Content>
			</Card.Root>
		</div>
	</section>

	<header>
		<Card.Root class="overflow-hidden">
			<Card.Header icon={StatsIcon} class="items-start">
				<div class="flex w-full min-w-0 flex-col gap-2">
					<h2 class="text-lg font-semibold tracking-tight">Overview</h2>
					<p class="text-muted-foreground text-sm">Live metrics and quick operations for your active environment.</p>
				</div>
			</Card.Header>
			<Card.Content class="space-y-2.5 pt-0 pb-3">
				<div class={`grid grid-cols-2 gap-2 md:grid-cols-4 ${gpuMetric !== null ? 'xl:grid-cols-5' : 'xl:grid-cols-4'}`}>
					<div class="bg-muted/15 rounded-md border px-3 py-2">
						<p class="text-muted-foreground flex items-center gap-1 text-[11px] font-medium uppercase">
							<CpuIcon class="size-3.5" />
							CPU
						</p>
						<p class="mt-0.5 text-sm font-semibold">{formatPercent(cpuMetric)}</p>
						<p class="text-muted-foreground text-[11px]">{cpuMetricLabel}</p>
						<div class="bg-muted mt-1.5 h-1 rounded-full">
							<div class="bg-primary h-full rounded-full" style={meterWidth(cpuMetric)}></div>
						</div>
					</div>

					<div class="bg-muted/15 rounded-md border px-3 py-2">
						<p class="text-muted-foreground flex items-center gap-1 text-[11px] font-medium uppercase">
							<MemoryStickIcon class="size-3.5" />
							Memory
						</p>
						<p class="mt-0.5 text-sm font-semibold">{formatPercent(memoryMetric)}</p>
						<p class="text-muted-foreground truncate text-[11px]">{memoryMetricLabel}</p>
						<div class="bg-muted mt-1.5 h-1 rounded-full">
							<div class="bg-primary h-full rounded-full" style={meterWidth(memoryMetric)}></div>
						</div>
					</div>

					<div class="bg-muted/15 rounded-md border px-3 py-2">
						<p class="text-muted-foreground flex items-center gap-1 text-[11px] font-medium uppercase">
							<VolumesIcon class="size-3.5" />
							Disk
						</p>
						<p class="mt-0.5 text-sm font-semibold">{formatPercent(diskMetric)}</p>
						<p class="text-muted-foreground truncate text-[11px]">{diskMetricLabel}</p>
						<div class="bg-muted mt-1.5 h-1 rounded-full">
							<div class="bg-primary h-full rounded-full" style={meterWidth(diskMetric)}></div>
						</div>
					</div>

					<div class="bg-muted/15 rounded-md border px-3 py-2">
						<p class="text-muted-foreground flex items-center gap-1 text-[11px] font-medium uppercase">
							<ContainersIcon class="size-3.5" />
							Containers
						</p>
						<p class="mt-0.5 text-sm font-semibold">{containerHealthPercent}%</p>
						<p class="text-muted-foreground text-[11px]">{runningContainers}/{totalContainers} running</p>
					</div>

					{#if gpuMetric !== null}
						<div class="bg-muted/15 rounded-md border px-3 py-2">
							<p class="text-muted-foreground flex items-center gap-1 text-[11px] font-medium uppercase">
								<GpuIcon class="size-3.5" />
								GPU
							</p>
							<p class="mt-0.5 text-sm font-semibold">{formatPercent(gpuMetric)}</p>
							<p class="text-muted-foreground text-[11px]">{gpuMetricLabel}</p>
							<div class="bg-muted mt-1.5 h-1 rounded-full">
								<div class="bg-primary h-full rounded-full" style={meterWidth(gpuMetric)}></div>
							</div>
						</div>
					{/if}
				</div>

				<div class="mt-1 flex flex-col gap-2 border-t pt-3 md:flex-row md:items-center md:justify-between">
					<div class="min-w-0 space-y-1">
						<div class="text-sm font-medium">{m.docker_engine_title({ engine: dockerInfo?.Name ?? 'Docker Engine' })}</div>
						<div class="text-muted-foreground flex flex-wrap items-center gap-2 text-xs">
							<span class="inline-flex items-center gap-1.5">
								<ContainersIcon class="size-3" />
								<span class="font-medium text-emerald-600">{runningContainers}</span>
								<span class="text-muted-foreground/70">/</span>
								<span>{totalContainers}</span>
							</span>
							<span class="text-muted-foreground/50">•</span>
							<span class="inline-flex items-center gap-1.5">
								<ImagesIcon class="size-3" />
								<span>{images.pagination.totalItems}</span>
								<span class="text-muted-foreground/70">{m.images_title().toLowerCase()}</span>
							</span>
							<span class="text-muted-foreground/50">•</span>
							<span>{imagesInUseCount} used · {imagesUnusedCount} unused</span>
							<span class="text-muted-foreground/50">•</span>
							<span class="font-mono">{dockerInfo?.OperatingSystem ?? '-'} / {dockerInfo?.Architecture ?? '-'}</span>
						</div>
					</div>

					{#if dockerInfo}
						<ArcaneButton
							action="base"
							tone="ghost"
							size="sm"
							icon={InfoIcon}
							customLabel="Docker info"
							class="h-7 px-2.5 text-xs"
							onclick={() => (dockerInfoDialogOpen = true)}
						/>
					{/if}
				</div>
			</Card.Content>
		</Card.Root>
	</header>

	<section class="flex min-h-0 flex-1 flex-col">
		<div class="mb-3 flex items-end justify-between gap-3">
			<div>
				<h2 class="text-lg font-semibold tracking-tight">Resource snapshots</h2>
				<p class="text-muted-foreground text-sm">Quickly inspect the most relevant container and image rows.</p>
			</div>
			<div class="hidden items-center gap-2 md:flex">
				<ArcaneButton action="base" tone="ghost" size="sm" href="/containers">
					{m.containers_title()}
				</ArcaneButton>
				<ArcaneButton action="base" tone="ghost" size="sm" href="/images">
					{m.images_title()}
				</ArcaneButton>
			</div>
		</div>
		<div class="grid min-h-0 flex-1 grid-cols-1 gap-4 lg:grid-cols-2">
			<DashboardContainerTable bind:containers isLoading={isLoading.loadingContainers} />
			<DashboardImageTable bind:images isLoading={isLoading.loadingImages} />
		</div>
	</section>

	<DockerInfoDialog bind:open={dockerInfoDialogOpen} {dockerInfo} />

	<PruneConfirmationDialog
		bind:open={isPruneDialogOpen}
		isPruning={isLoading.pruning}
		imagePruneMode={(settings?.dockerPruneMode as 'dangling' | 'all') || 'dangling'}
		onConfirm={confirmPrune}
		onCancel={() => (isPruneDialogOpen = false)}
	/>
</div>
