<script lang="ts">
	import { toast } from 'svelte-sonner';
	import PruneConfirmationDialog from '$lib/components/dialogs/prune-confirmation-dialog.svelte';
	import DockerInfoDialog from '$lib/components/dialogs/docker-info-dialog.svelte';
	import * as Card from '$lib/components/ui/card/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { onMount } from 'svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import userStore from '$lib/stores/user-store';
	import { createStatsWebSocket } from '$lib/utils/ws';
	import type { ReconnectingWebSocket } from '$lib/utils/ws';
	import type { User } from '$lib/types/user.type';
	import QuickActions from '$lib/components/quick-actions.svelte';
	import type { SystemStats } from '$lib/types/system-stats.type';
	import DashboardActionCard from './dash-action-card.svelte';
	import DashboardMetricTile from './dash-metric-tile.svelte';
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
		VolumesIcon,
		ContainersIcon,
		ImagesIcon,
		GpuIcon,
		InfoIcon,
		ApiKeyIcon,
		CheckIcon
	} from '$lib/icons';

	let { data } = $props();
	let containers = $derived(data.containers);
	let images = $derived(data.images);
	let dockerInfo = $derived(data.dockerInfo);
	let containerStatusCounts = $derived(data.containerStatusCounts);
	let settings = $derived(data.settings);
	let imageUsageCounts = $derived(
		(
			data as {
				imageUsageCounts?: { imagesInuse?: number; imagesUnused?: number } | null;
			}
		).imageUsageCounts
	);
	let dashboardActionItems = $derived(
		(
			data as {
				dashboardActionItems?: { items?: Array<{ kind: string; count: number }> } | null;
			}
		).dashboardActionItems
	);
	let debugAllGood = $derived((data as { debugAllGood?: boolean }).debugAllGood ?? false);
	let initialUserFromData = $derived((data as { user?: User | null }).user ?? null);
	let currentUser = $state<User | null>(null);

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

	const dashboardAttentionCounts = $derived.by(() => {
		const counts = {
			stoppedContainers: 0,
			imageUpdates: 0,
			actionableVulnerabilities: 0,
			expiringApiKeys: 0
		};

		for (const item of dashboardActionItems?.items ?? []) {
			if (item.kind === 'stopped_containers') {
				counts.stoppedContainers = item.count;
			} else if (item.kind === 'image_updates') {
				counts.imageUpdates = item.count;
			} else if (item.kind === 'actionable_vulnerabilities') {
				counts.actionableVulnerabilities = item.count;
			} else if (item.kind === 'expiring_keys') {
				counts.expiringApiKeys = item.count;
			}
		}

		return counts;
	});

	const stoppedContainersAttentionCount = $derived(dashboardAttentionCounts.stoppedContainers);
	const imageUpdatesAttentionCount = $derived(dashboardAttentionCounts.imageUpdates);
	const actionableVulnerabilitiesAttentionCount = $derived(dashboardAttentionCounts.actionableVulnerabilities);
	const expiringApiKeysAttentionCount = $derived(dashboardAttentionCounts.expiringApiKeys);

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
	const diskRiskLabel = $derived.by(() => {
		if (diskRisk === 'critical') return m.vuln_severity_critical();
		if (diskRisk === 'high') return m.vuln_severity_high();
		if (diskRisk === 'moderate') return m.vuln_severity_medium();
		if (diskRisk === 'healthy') return m.vuln_clean();
		return m.common_unknown();
	});
	const diskRiskBadgeVariant = $derived.by(() => {
		if (diskRisk === 'critical') return 'red';
		if (diskRisk === 'high') return 'orange';
		if (diskRisk === 'moderate') return 'amber';
		if (diskRisk === 'healthy') return 'green';
		return 'gray';
	});
	const hasDiskPressureAlert = $derived(!debugAllGood && (diskRisk === 'critical' || diskRisk === 'high'));
	const showNeedsAttention = $derived(
		!debugAllGood &&
			(stoppedContainersAttentionCount > 0 ||
				imageUpdatesAttentionCount > 0 ||
				actionableVulnerabilitiesAttentionCount > 0 ||
				expiringApiKeysAttentionCount > 0 ||
				hasDiskPressureAlert)
	);
	const attentionItemsCount = $derived(
		debugAllGood
			? 0
			: (stoppedContainersAttentionCount > 0 ? 1 : 0) +
					(imageUpdatesAttentionCount > 0 ? 1 : 0) +
					(actionableVulnerabilitiesAttentionCount > 0 ? 1 : 0) +
					(expiringApiKeysAttentionCount > 0 ? 1 : 0) +
					(hasDiskPressureAlert ? 1 : 0)
	);
	const stoppedContainersBadgeText = $derived(`${stoppedContainersAttentionCount} ${m.common_stopped()}`);
	const imageUpdatesBadgeText = $derived(`${imageUpdatesAttentionCount} ${m.common_pending()}`);
	const actionableVulnerabilitiesBadgeText = $derived(`${m.vuln_severity_critical()} + ${m.vuln_severity_high()}`);
	const expiringApiKeysBadgeText = $derived(`${expiringApiKeysAttentionCount} ${m.common_pending()}`);
	const containerHealthLabel = $derived(`${runningContainers}/${totalContainers} ${m.common_running()}`);
	const imageUsageLabel = $derived(`${imagesInUseCount} ${m.common_in_use()} · ${imagesUnusedCount} ${m.common_unused()}`);
	const diskPressureValue = $derived(diskUsagePercent === null ? '--' : `${diskUsagePercent.toFixed(1)}%`);
	const diskPressureDescription = $derived.by(() => {
		if (currentStats?.diskUsage !== undefined && currentStats?.diskTotal) {
			return `${bytes.format(currentStats.diskUsage, { unitSeparator: ' ' }) ?? '-'} / ${bytes.format(currentStats.diskTotal, { unitSeparator: ' ' }) ?? '-'}`;
		}
		return m.common_loading();
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
		return count > 0 ? `${count} ${count === 1 ? m.dashboard_meter_gpu_device() : m.dashboard_meter_gpu_devices()}` : '--';
	});
	const greetingBase = $derived.by(() => {
		const hour = new Date().getHours();
		if (hour >= 5 && hour < 12) return m.dashboard_greeting_morning();
		if (hour >= 12 && hour < 18) return m.dashboard_greeting_afternoon();
		if (hour >= 18 && hour < 23) return m.dashboard_greeting_evening();
		return m.dashboard_greeting_welcome_back();
	});
	const greetingUserName = $derived.by(() => {
		const name = currentUser?.displayName?.trim() || currentUser?.username?.trim() || '';
		return name;
	});
	const dashboardHeroGreeting = $derived.by(() =>
		greetingUserName
			? m.dashboard_greeting_with_name({ greeting: greetingBase, name: greetingUserName })
			: m.dashboard_greeting_without_name({ greeting: greetingBase })
	);

	$effect(() => {
		if (!currentUser && initialUserFromData) {
			currentUser = initialUserFromData;
		}
	});

	function formatPercent(value: number | null): string {
		return value === null ? '--' : `${value.toFixed(1)}%`;
	}

	async function refreshData() {
		isLoading.refreshing = true;
		await invalidateAll();
		isLoading.refreshing = false;
	}

	onMount(() => {
		let mounted = true;
		const unsubscribeUser = userStore.subscribe((user) => {
			currentUser = user ?? initialUserFromData;
		});

		(async () => {
			await environmentStore.ready;

			if (mounted) {
				setupStatsWS();
			}
		})();

		return () => {
			mounted = false;
			unsubscribeUser();
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
	<header
		class="border-border/70 bg-card/75 dark:border-surface/70 dark:bg-surface/25 rounded-xl border p-4 shadow-sm supports-[backdrop-filter]:backdrop-blur-sm sm:p-5"
	>
		<div class="relative flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
			<div class="space-y-1.5">
				<p class="text-muted-foreground text-[11px] font-semibold tracking-[0.14em] uppercase">{m.dashboard_title()}</p>
				<h1 class="text-2xl font-bold tracking-tight sm:text-3xl">{dashboardHeroGreeting}</h1>
				<p class="text-muted-foreground text-sm">
					{m.dashboard_hero_summary({ running: runningContainers, total: totalContainers, images: images.pagination.totalItems })}
				</p>
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
		</div>
	</header>

	<section class="space-y-1.5">
		{#if attentionItemsCount > 0}
			<div class="flex flex-col gap-1">
				<h2 class="text-lg font-semibold tracking-tight">{m.dashboard_action_items_title()}</h2>
			</div>
			<div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
				{#if stoppedContainersAttentionCount > 0}
					<DashboardActionCard
						title={m.containers_title()}
						icon={ContainersIcon}
						badgeText={stoppedContainersBadgeText}
						badgeVariant="red"
						value={`${runningContainers}/${totalContainers}`}
						description={m.common_running()}
						ctaLabel={m.common_view_all()}
						href="/containers"
					/>
				{/if}

				{#if imageUpdatesAttentionCount > 0}
					<DashboardActionCard
						title={m.images_updates()}
						icon={UpdateIcon}
						badgeText={imageUpdatesBadgeText}
						badgeVariant="amber"
						value={imageUpdatesAttentionCount}
						description={m.image_update_tag_description()}
						ctaLabel={m.common_view_all()}
						href="/images"
					/>
				{/if}

				{#if actionableVulnerabilitiesAttentionCount > 0}
					<DashboardActionCard
						title={m.security_title()}
						icon={AlertTriangleIcon}
						badgeText={actionableVulnerabilitiesBadgeText}
						badgeVariant="red"
						value={actionableVulnerabilitiesAttentionCount}
						description={m.security_subtitle()}
						ctaLabel={m.common_view()}
						href="/security"
					/>
				{/if}

				{#if hasDiskPressureAlert}
					<DashboardActionCard
						title={m.dashboard_meter_disk()}
						icon={VolumesIcon}
						badgeText={diskRiskLabel}
						badgeVariant={diskRiskBadgeVariant}
						value={diskPressureValue}
						description={diskPressureDescription}
						ctaLabel={m.common_view_all()}
						href="/volumes"
					/>
				{/if}

				{#if expiringApiKeysAttentionCount > 0}
					<DashboardActionCard
						title={m.api_key_page_title()}
						icon={ApiKeyIcon}
						badgeText={expiringApiKeysBadgeText}
						badgeVariant="orange"
						value={expiringApiKeysAttentionCount}
						description={m.api_key_expires_at_description()}
						ctaLabel={m.common_view_all()}
						href="/settings/api-keys"
					/>
				{/if}
			</div>
		{:else}
			<Card.Root variant="outlined" class="border-emerald-500/30 bg-emerald-500/5 shadow-sm">
				<Card.Content class="space-y-2.5 p-4">
					<div class="flex items-center gap-2 text-sm font-semibold text-emerald-700 dark:text-emerald-300">
						<CheckIcon class="size-4" />
						<span>{m.progress_deploy_service_healthy({ service: m.environments_title() })}</span>
					</div>
					<p class="text-muted-foreground text-xs leading-relaxed">{m.dashboard_no_actionable_events()}</p>
				</Card.Content>
			</Card.Root>
		{/if}
	</section>

	<header>
		<Card.Root class="overflow-hidden">
			<Card.Header icon={StatsIcon} class="items-start">
				<div class="flex w-full min-w-0 flex-col gap-2">
					<h2 class="text-lg font-semibold tracking-tight">{m.common_overview()}</h2>
					<p class="text-muted-foreground text-sm">{m.dashboard_overview_caption()}</p>
				</div>
			</Card.Header>
			<Card.Content class="space-y-2.5 pt-0 pb-3">
				<div
					class={`grid grid-cols-2 gap-1 md:grid-cols-3 md:gap-1.5 ${gpuMetric !== null ? 'xl:grid-cols-4' : 'xl:grid-cols-3'}`}
				>
					<DashboardMetricTile
						title={m.dashboard_meter_cpu()}
						icon={CpuIcon}
						value={formatPercent(cpuMetric)}
						label={cpuMetricLabel}
						meterValue={cpuMetric}
					/>

					<DashboardMetricTile
						title={m.dashboard_meter_memory()}
						icon={MemoryStickIcon}
						value={formatPercent(memoryMetric)}
						label={memoryMetricLabel}
						labelClass="truncate"
						meterValue={memoryMetric}
					/>

					<DashboardMetricTile
						title={m.dashboard_meter_disk()}
						icon={VolumesIcon}
						value={formatPercent(diskMetric)}
						label={diskMetricLabel}
						labelClass="truncate"
						meterValue={diskMetric}
					/>

					{#if gpuMetric !== null}
						<DashboardMetricTile
							title={m.dashboard_meter_gpu()}
							icon={GpuIcon}
							value={formatPercent(gpuMetric)}
							label={gpuMetricLabel}
							meterValue={gpuMetric}
						/>
					{/if}
				</div>

				<div class="mt-1 flex flex-col gap-2 border-t pt-3 md:flex-row md:items-center md:justify-between">
					<div class="min-w-0 space-y-1">
						<div class="text-sm font-medium">{m.docker_engine_title({ engine: dockerInfo?.Name ?? m.common_unknown() })}</div>
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
								<span class="text-muted-foreground/70">{m.images_title()}</span>
							</span>
							<span class="text-muted-foreground/50">•</span>
							<span>{imageUsageLabel}</span>
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
							showLabel={false}
							customLabel={m.common_inspect()}
							class="h-7 px-2.5 text-xs"
							onclick={() => (dockerInfoDialogOpen = true)}
						/>
					{/if}
				</div>
			</Card.Content>
		</Card.Root>
	</header>

	<section class="flex min-h-0 flex-1 flex-col">
		<div class="mb-3 flex items-center justify-between gap-3">
			<h2 class="text-lg font-semibold tracking-tight">{m.dashboard_resource_tables_title()}</h2>
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
