<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { formatDistanceToNow } from 'date-fns';
	import { onDestroy } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { ActionButtonGroup, type ActionButton } from '$lib/components/action-button-group/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import PruneConfirmationDialog from '$lib/components/dialogs/prune-confirmation-dialog.svelte';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import * as Card from '$lib/components/ui/card/index.js';
	import { m } from '$lib/paraglide/messages';
	import { dashboardService } from '$lib/services/dashboard-service';
	import { systemService } from '$lib/services/system-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import userStore from '$lib/stores/user-store';
	import type { SystemStats } from '$lib/types/system-stats.type';
	import type {
		DashboardActionItem,
		DashboardEnvironmentCardState,
		DashboardEnvironmentOverview,
		DashboardOverviewSummary,
		DashboardSnapshot
	} from '$lib/types/dashboard.type';
	import type { Environment } from '$lib/types/environment.type';
	import type { PruneType, SystemPruneRequest } from '$lib/types/prune.type';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import { tryCatch } from '$lib/utils/try-catch';
	import { getEnvironmentStatusVariant, isEnvironmentOnline, resolveEnvironmentStatus } from '$lib/utils/environment-status';
	import { createStatsWebSocket, type ReconnectingWebSocket } from '$lib/utils/ws';
	import bytes from '$lib/utils/bytes';
	import { fromStore } from 'svelte/store';
	import {
		ContainersIcon,
		CpuIcon,
		EnvironmentsIcon,
		ImagesIcon,
		InspectIcon,
		MemoryStickIcon,
		RefreshIcon,
		TrashIcon,
		VolumesIcon
	} from '$lib/icons';
	import DashboardMetricTile from './dash-metric-tile.svelte';
	import DashboardEnvironmentUpgradeAction from './dashboard-environment-upgrade-action.svelte';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';

	let {
		heroGreeting,
		debugAllGood = false,
		debugUpgrade = false
	}: {
		heroGreeting: string;
		debugAllGood?: boolean;
		debugUpgrade?: boolean;
	} = $props();

	const emptySnapshotSettings: DashboardSnapshot['settings'] = {};

	type EnvironmentLiveStatsState = {
		stats: SystemStats | null;
		loading: boolean;
		hasLoaded: boolean;
		client: ReconnectingWebSocket<SystemStats> | null;
	};

	let isRefreshing = $state(false);
	let isPruneDialogOpen = $state(false);
	let pruneEnvironment = $state<DashboardEnvironmentOverview | null>(null);
	let pruningEnvironmentId = $state<string | null>(null);
	let reloadVersion = $state(0);
	let liveStatsByEnvironmentId = $state<Record<string, EnvironmentLiveStatsState>>({});
	let upgradeDialogOpenById = $state<Record<string, boolean>>({});
	let upgradeDialogUpgradingById = $state<Record<string, boolean>>({});

	function getUpgradeOpen(id: string): boolean {
		return upgradeDialogOpenById[id] ?? false;
	}
	function setUpgradeOpen(id: string, value: boolean): void {
		upgradeDialogOpenById[id] = value;
	}
	function getUpgradeUpgrading(id: string): boolean {
		return upgradeDialogUpgradingById[id] ?? false;
	}
	function setUpgradeUpgrading(id: string, value: boolean): void {
		upgradeDialogUpgradingById[id] = value;
	}

	const storeUser = fromStore(userStore);
	const availableEnvironments = $derived(environmentStore.available);
	const currentEnvironmentId = $derived(environmentStore.selected?.id ?? null);
	const currentUserIsAdmin = $derived(!!storeUser.current?.roles?.includes('admin'));

	function shouldLoadEnvironment(environment: Environment): boolean {
		return environment.enabled && isEnvironmentOnline(environment);
	}

	function createBaseEnvironmentOverview(environment: Environment): DashboardEnvironmentOverview {
		return {
			environment,
			containers: { runningContainers: 0, stoppedContainers: 0, totalContainers: 0 },
			imageUsageCounts: { imagesInuse: 0, imagesUnused: 0, totalImages: 0, totalImageSize: 0 },
			actionItems: { items: [] },
			settings: emptySnapshotSettings,
			snapshotState: 'skipped'
		};
	}

	function getEnvironmentCardSortRank(environment: Environment): number {
		if (shouldLoadEnvironment(environment)) {
			return 0;
		}

		if (environment.enabled) {
			return 1;
		}

		return 2;
	}

	function buildEnvironmentLoadPromise(
		environment: Environment,
		currentDebugFlag: boolean
	): Promise<DashboardEnvironmentOverview> {
		return dashboardService
			.getDashboardForEnvironment(environment.id, {
				debugAllGood: currentDebugFlag
			})
			.then((snapshot) => ({
				environment,
				containers: snapshot.containers.counts ?? { runningContainers: 0, stoppedContainers: 0, totalContainers: 0 },
				imageUsageCounts: snapshot.imageUsageCounts,
				actionItems: snapshot.actionItems,
				settings: snapshot.settings,
				snapshotState: 'ready' as const
			}))
			.catch((error) => ({
				environment,
				containers: { runningContainers: 0, stoppedContainers: 0, totalContainers: 0 },
				imageUsageCounts: { imagesInuse: 0, imagesUnused: 0, totalImages: 0, totalImageSize: 0 },
				actionItems: { items: [] },
				settings: emptySnapshotSettings,
				snapshotState: 'error' as const,
				snapshotError: extractApiErrorMessage(error)
			}));
	}

	function buildOverviewSummaryFromItemsInternal(settledEnvironments: DashboardEnvironmentOverview[]): DashboardOverviewSummary {
		return {
			totalEnvironments: settledEnvironments.length,
			reachableEnvironments: settledEnvironments.filter(
				(item) => item.environment.enabled && isEnvironmentOnline(item.environment)
			).length,
			unavailableEnvironments: settledEnvironments.filter(
				(item) => item.environment.enabled && !isEnvironmentOnline(item.environment)
			).length,
			disabledEnvironments: settledEnvironments.filter((item) => !item.environment.enabled).length,
			totalContainers: settledEnvironments.reduce((total, item) => total + item.containers.totalContainers, 0),
			runningContainers: settledEnvironments.reduce((total, item) => total + item.containers.runningContainers, 0),
			stoppedContainers: settledEnvironments.reduce((total, item) => total + item.containers.stoppedContainers, 0),
			totalImages: settledEnvironments.reduce((total, item) => total + item.imageUsageCounts.totalImages, 0),
			imagesInUse: settledEnvironments.reduce((total, item) => total + item.imageUsageCounts.imagesInuse, 0),
			imagesUnused: settledEnvironments.reduce((total, item) => total + item.imageUsageCounts.imagesUnused, 0),
			totalImageSize: settledEnvironments.reduce((total, item) => total + item.imageUsageCounts.totalImageSize, 0)
		};
	}

	function mapOverviewSummary(
		summary: import('$lib/types/dashboard.type').DashboardEnvironmentsSummary
	): DashboardOverviewSummary {
		return {
			totalEnvironments: summary.totalEnvironments,
			reachableEnvironments: summary.onlineEnvironments + summary.standbyEnvironments,
			unavailableEnvironments: summary.offlineEnvironments + summary.pendingEnvironments + summary.errorEnvironments,
			disabledEnvironments: summary.disabledEnvironments,
			totalContainers: summary.containers.totalContainers,
			runningContainers: summary.containers.runningContainers,
			stoppedContainers: summary.containers.stoppedContainers,
			totalImages: summary.imageUsageCounts.totalImages,
			imagesInUse: summary.imageUsageCounts.imagesInuse,
			imagesUnused: summary.imageUsageCounts.imagesUnused,
			totalImageSize: summary.imageUsageCounts.totalImageSize
		};
	}

	function createEmptyLiveStatsState(): EnvironmentLiveStatsState {
		return {
			stats: null,
			loading: true,
			hasLoaded: false,
			client: null
		};
	}

	function ensureEnvironmentLiveStats(environment: Environment) {
		if (!shouldLoadEnvironment(environment)) {
			removeEnvironmentLiveStats(environment.id);
			return;
		}

		if (!liveStatsByEnvironmentId[environment.id]) {
			liveStatsByEnvironmentId[environment.id] = createEmptyLiveStatsState();
		}

		const liveStatsState = liveStatsByEnvironmentId[environment.id];
		if (!liveStatsState) {
			return;
		}

		if (liveStatsState.client) {
			return;
		}

		liveStatsState.loading = !liveStatsState.hasLoaded;
		liveStatsState.client = createStatsWebSocket({
			getEnvId: () => environment.id,
			onOpen: () => {
				if (!liveStatsState.hasLoaded) {
					liveStatsState.loading = true;
				}
			},
			onMessage: (stats) => {
				liveStatsState.stats = stats;
				liveStatsState.hasLoaded = true;
				liveStatsState.loading = false;
			},
			onError: (error) => {
				console.error(`Stats websocket error for environment ${environment.id}:`, error);
			}
		});
		liveStatsState.client.connect();
	}

	function removeEnvironmentLiveStats(environmentId: string) {
		const liveStatsState = liveStatsByEnvironmentId[environmentId];
		if (!liveStatsState) {
			return;
		}

		liveStatsState.client?.close();
		delete liveStatsByEnvironmentId[environmentId];
	}

	function cleanupEnvironmentLiveStats() {
		for (const environmentId of Object.keys(liveStatsByEnvironmentId)) {
			removeEnvironmentLiveStats(environmentId);
		}
	}

	const environmentCards = $derived.by((): DashboardEnvironmentCardState[] => {
		const refreshNonce = reloadVersion;
		void refreshNonce;

		return availableEnvironments
			.map((environment, index) => ({
				environment,
				index,
				sortRank: getEnvironmentCardSortRank(environment)
			}))
			.sort((a, b) => {
				if (a.sortRank !== b.sortRank) {
					return a.sortRank - b.sortRank;
				}

				return a.index - b.index;
			})
			.map(({ environment }) => ({ environment }));
	});

	const environmentBoardStatePromise = $derived.by(
		async (): Promise<{
			overviewById: Map<string, DashboardEnvironmentOverview>;
			summary: DashboardOverviewSummary;
		}> => {
			void reloadVersion;

			const currentEnvironmentCards = environmentCards;
			const result = await tryCatch(dashboardService.getDashboardEnvironmentsOverview({ debugAllGood }));
			if (!result.error) {
				return {
					overviewById: new Map(result.data.environments.map((item) => [item.environment.id, item])),
					summary: mapOverviewSummary(result.data.summary)
				};
			}

			const fallbackItems = await Promise.all(
				currentEnvironmentCards.map((item) =>
					shouldLoadEnvironment(item.environment)
						? buildEnvironmentLoadPromise(item.environment, debugAllGood)
						: Promise.resolve(createBaseEnvironmentOverview(item.environment))
				)
			);

			return {
				overviewById: new Map(fallbackItems.map((item) => [item.environment.id, item])),
				summary: buildOverviewSummaryFromItemsInternal(fallbackItems)
			};
		}
	);

	$effect(() => {
		const reachableEnvironmentIds: string[] = [];

		for (const item of environmentCards) {
			if (!shouldLoadEnvironment(item.environment)) {
				continue;
			}

			reachableEnvironmentIds.push(item.environment.id);
			ensureEnvironmentLiveStats(item.environment);
		}

		for (const environmentId of Object.keys(liveStatsByEnvironmentId)) {
			if (!reachableEnvironmentIds.includes(environmentId)) {
				removeEnvironmentLiveStats(environmentId);
			}
		}
	});

	onDestroy(() => {
		cleanupEnvironmentLiveStats();
	});

	async function refreshOverview() {
		isRefreshing = true;
		try {
			await invalidateAll();
			reloadVersion += 1;
		} finally {
			isRefreshing = false;
		}
	}

	async function useEnvironment(environment: Environment) {
		if (!environment.enabled) {
			toast.error(m.environments_cannot_switch_disabled());
			return;
		}

		if (!isEnvironmentOnline(environment)) {
			toast.error(m.common_unavailable());
			return;
		}

		try {
			await environmentStore.setEnvironment(environment);
			toast.success(m.environments_switched_to({ name: environment.name }));
		} catch (error) {
			console.error('Failed to switch environment:', error);
			toast.error(m.common_update_failed({ resource: m.resource_environment() }));
		}
	}

	function getTransportBadge(environment: Environment): { text: string; variant: 'blue' | 'purple' | 'gray' } {
		if (!environment.isEdge) {
			return { text: m.dashboard_all_transport_http(), variant: 'gray' };
		}

		if (environment.lastPollAt) {
			return { text: m.environments_edge_polling_label(), variant: 'blue' };
		}

		if (!environment.connected || !environment.edgeTransport) {
			return { text: m.dashboard_all_transport_edge(), variant: 'gray' };
		}

		return environment.edgeTransport === 'websocket'
			? { text: m.dashboard_all_transport_websocket(), variant: 'purple' }
			: { text: m.dashboard_all_transport_grpc(), variant: 'blue' };
	}

	function getResolvedStatusLabel(environment: Environment): string {
		switch (resolveEnvironmentStatus(environment)) {
			case 'online':
				return m.common_online();
			case 'standby':
				return m.common_standby();
			case 'pending':
				return m.common_pending();
			case 'error':
				return m.common_error();
			default:
				return m.common_offline();
		}
	}

	function getActionItemLabel(item: DashboardActionItem): string {
		switch (item.kind) {
			case 'stopped_containers':
				return m.containers_title();
			case 'image_updates':
				return m.images_updates();
			case 'actionable_vulnerabilities':
				return m.security_title();
			case 'expiring_keys':
				return m.api_key_page_title();
			default:
				return m.common_unknown();
		}
	}

	function getActionSummary(item: DashboardEnvironmentOverview): string {
		if (debugAllGood || item.actionItems.items.length === 0) {
			return m.dashboard_no_actionable_events();
		}

		return item.actionItems.items
			.slice(0, 2)
			.map((actionItem) => `${actionItem.count} ${getActionItemLabel(actionItem)}`)
			.join(' · ');
	}

	function getActivityMeta(environment: Environment): { label: string; value: string; title: string } {
		if (!isEnvironmentOnline(environment)) {
			const statusLabel = getResolvedStatusLabel(environment);
			return {
				label: m.dashboard_all_activity(),
				value: statusLabel,
				title: statusLabel
			};
		}

		const labelAndValue = environment.lastHeartbeat
			? { label: m.environments_edge_last_heartbeat_label(), raw: environment.lastHeartbeat }
			: environment.lastPollAt
				? { label: m.environments_edge_last_poll_label(), raw: environment.lastPollAt }
				: environment.connectedAt
					? { label: m.environments_edge_connected_since_label(), raw: environment.connectedAt }
					: environment.lastSeen
						? { label: m.dashboard_all_last_seen(), raw: environment.lastSeen }
						: null;

		if (!labelAndValue?.raw) {
			return { label: m.dashboard_all_activity(), value: m.common_never(), title: m.common_never() };
		}

		const parsed = new Date(labelAndValue.raw);
		if (Number.isNaN(parsed.getTime())) {
			return { label: labelAndValue.label, value: m.common_unknown(), title: m.common_unknown() };
		}

		return {
			label: labelAndValue.label,
			value: formatDistanceToNow(parsed, { addSuffix: true }),
			title: parsed.toLocaleString()
		};
	}

	function formatPercent(value: number | null | undefined): string {
		return value === null || value === undefined ? '--' : `${value.toFixed(1)}%`;
	}

	function getLiveStatsState(environmentId: string): EnvironmentLiveStatsState | null {
		return liveStatsByEnvironmentId[environmentId] ?? null;
	}

	function getCpuMetric(stats: SystemStats | null): number | null {
		return stats?.cpuUsage ?? null;
	}

	function getMemoryMetric(stats: SystemStats | null): number | null {
		if (stats?.memoryUsage === undefined || !stats.memoryTotal) {
			return null;
		}

		return (stats.memoryUsage / stats.memoryTotal) * 100;
	}

	function getDiskMetric(stats: SystemStats | null): number | null {
		if (stats?.diskUsage === undefined || !stats.diskTotal || stats.diskTotal <= 0) {
			return null;
		}

		return (stats.diskUsage / stats.diskTotal) * 100;
	}

	function getCpuMetricLabel(stats: SystemStats | null): string {
		if (!stats) {
			return '--';
		}

		return `${stats.cpuCount ?? 0} ${m.common_cpus()}`;
	}

	function getCapacityLabel(used: number | undefined, total: number | undefined): string {
		if (used === undefined || total === undefined || total <= 0) {
			return '--';
		}

		return `${bytes.format(used, { unitSeparator: ' ' }) ?? '-'} / ${bytes.format(total, { unitSeparator: ' ' }) ?? '-'}`;
	}

	function canPruneEnvironment(item: DashboardEnvironmentOverview): boolean {
		return (
			currentUserIsAdmin && item.environment.enabled && item.snapshotState === 'ready' && isEnvironmentOnline(item.environment)
		);
	}

	function getEnvironmentActionButtons(item: DashboardEnvironmentOverview, isCurrent: boolean): ActionButton[] {
		const buttons: ActionButton[] = [];

		if (currentUserIsAdmin) {
			buttons.push({
				id: `${item.environment.id}-prune`,
				action: 'prune',
				label: m.quick_actions_prune_system(),
				loading: pruningEnvironmentId === item.environment.id,
				disabled: !canPruneEnvironment(item) || !!pruningEnvironmentId,
				onclick: () => openPruneDialog(item),
				icon: TrashIcon
			});
		}

		buttons.push({
			id: `${item.environment.id}-use`,
			action: 'base',
			label: m.environments_use_environment(),
			disabled: !shouldLoadEnvironment(item.environment) || isCurrent,
			onclick: () => void useEnvironment(item.environment),
			icon: EnvironmentsIcon
		});

		buttons.push({
			id: `${item.environment.id}-details`,
			action: 'inspect',
			label: m.common_view_details(),
			onclick: () => void goto(`/environments/${item.environment.id}`),
			icon: InspectIcon
		});

		return buttons;
	}

	function formatEnvironmentOverviewLabel(summary: DashboardOverviewSummary): string {
		if (summary.totalEnvironments === 0) {
			return m.dashboard_all_no_visible_environments();
		}

		const parts = [m.dashboard_all_reachable_summary({ count: summary.reachableEnvironments })];

		if (summary.unavailableEnvironments > 0) {
			parts.push(m.dashboard_all_unavailable_summary({ count: summary.unavailableEnvironments }));
		}

		if (summary.disabledEnvironments > 0) {
			parts.push(m.dashboard_all_disabled_summary({ count: summary.disabledEnvironments }));
		}

		return parts.join(' · ');
	}

	function formatContainerOverviewLabel(summary: DashboardOverviewSummary): string {
		if (summary.totalContainers === 0) {
			return m.dashboard_all_no_containers();
		}

		return m.dashboard_all_container_summary({ running: summary.runningContainers, stopped: summary.stoppedContainers });
	}

	function formatImageOverviewLabel(summary: DashboardOverviewSummary): string {
		if (summary.totalImages === 0) {
			return m.dashboard_all_no_images();
		}

		return m.dashboard_all_image_summary({ inUse: summary.imagesInUse, unused: summary.imagesUnused });
	}

	function formatStorageOverviewLabel(summary: DashboardOverviewSummary): string {
		if (summary.totalImageSize === 0) {
			return m.dashboard_all_no_storage();
		}

		if (summary.imagesUnused > 0) {
			return m.dashboard_all_unused_images_summary({ count: summary.imagesUnused });
		}

		return m.dashboard_all_images_tracked_summary({ count: summary.totalImages });
	}

	function openPruneDialog(item: DashboardEnvironmentOverview) {
		if (!canPruneEnvironment(item)) {
			return;
		}

		pruneEnvironment = item;
		isPruneDialogOpen = true;
	}

	function closePruneDialog() {
		if (pruningEnvironmentId) {
			return;
		}

		isPruneDialogOpen = false;
		pruneEnvironment = null;
	}

	async function confirmPrune(pruneRequest: SystemPruneRequest) {
		const selectedTypes = Object.keys(pruneRequest) as PruneType[];
		if (!pruneEnvironment || pruningEnvironmentId || selectedTypes.length === 0) {
			return;
		}

		const targetEnvironment = pruneEnvironment;
		const environmentId = targetEnvironment.environment.id;

		const typeLabels: Record<PruneType, string> = {
			containers: m.prune_stopped_containers(),
			images: m.prune_unused_images(),
			networks: m.prune_unused_networks(),
			volumes: m.prune_unused_volumes(),
			buildCache: m.build_cache()
		};
		const typesString = selectedTypes.map((type) => typeLabels[type]).join(', ');

		pruningEnvironmentId = environmentId;

		handleApiResultWithCallbacks({
			result: await tryCatch(systemService.pruneAllForEnvironment(environmentId, pruneRequest)),
			message: m.dashboard_prune_failed({ types: typesString }),
			setLoadingState: (value) => {
				pruningEnvironmentId = value ? environmentId : null;
			},
			onSuccess: async () => {
				isPruneDialogOpen = false;
				pruneEnvironment = null;
				if (selectedTypes.length === 1) {
					toast.success(m.dashboard_prune_success_one({ types: typesString }), {
						description: targetEnvironment.environment.name
					});
				} else {
					toast.success(m.dashboard_prune_success_many({ types: typesString }), {
						description: targetEnvironment.environment.name
					});
				}
				await refreshOverview();
			}
		});
	}
</script>

<div class="flex h-full min-h-0 flex-col gap-4 overflow-hidden pt-3 md:gap-5 md:pt-4">
	<header
		class="dark:border-surface/80 dark:bg-surface/10 shrink-0 rounded-xl border border-white/80 bg-white/10 p-4 shadow-sm backdrop-blur-sm sm:p-5"
	>
		<div class="relative flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
			<div class="space-y-1.5">
				<p class="text-muted-foreground text-[11px] font-semibold tracking-[0.14em] uppercase">{m.dashboard_title()}</p>
				<h1 class="text-2xl font-bold tracking-tight sm:text-3xl">{heroGreeting}</h1>
			</div>

			<ArcaneButton
				action="restart"
				size="sm"
				customLabel={m.common_refresh()}
				icon={RefreshIcon}
				loading={isRefreshing}
				onclick={refreshOverview}
			/>
		</div>
	</header>

	<section class="shrink-0 space-y-3">
		<h2 class="text-lg font-semibold tracking-tight">{m.common_overview()}</h2>

		{#await environmentBoardStatePromise}
			<div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
				{#each [1, 2, 3, 4] as tile (tile)}
					<div class="border-border/50 bg-background/50 rounded-xl border p-4">
						<Skeleton class="h-3 w-24" />
						<Skeleton class="mt-3 h-9 w-16" />
						<Skeleton class="mt-2 h-4 w-32" />
					</div>
				{/each}
			</div>
		{:then boardState}
			{@const summary = boardState.summary}
			<div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
				<div class="border-border/50 bg-background/50 rounded-xl border p-4">
					<div class="text-muted-foreground flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
						<EnvironmentsIcon class="size-3.5" />
						<span>{m.environments_title()}</span>
					</div>
					<div class="mt-3 text-3xl font-semibold tracking-tight tabular-nums">{summary.totalEnvironments}</div>
					<div class="text-muted-foreground mt-1 text-sm">{formatEnvironmentOverviewLabel(summary)}</div>
				</div>

				<div class="border-border/50 bg-background/50 rounded-xl border p-4">
					<div class="text-muted-foreground flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
						<ContainersIcon class="size-3.5" />
						<span>{m.containers_title()}</span>
					</div>
					<div class="mt-3 text-3xl font-semibold tracking-tight tabular-nums">{summary.totalContainers}</div>
					<div class="text-muted-foreground mt-1 text-sm">{formatContainerOverviewLabel(summary)}</div>
				</div>

				<div class="border-border/50 bg-background/50 rounded-xl border p-4">
					<div class="text-muted-foreground flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
						<ImagesIcon class="size-3.5" />
						<span>{m.images_title()}</span>
					</div>
					<div class="mt-3 text-3xl font-semibold tracking-tight tabular-nums">{summary.totalImages}</div>
					<div class="text-muted-foreground mt-1 text-sm">{formatImageOverviewLabel(summary)}</div>
				</div>

				<div class="border-border/50 bg-background/50 rounded-xl border p-4">
					<div class="text-muted-foreground flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
						<VolumesIcon class="size-3.5" />
						<span>{m.dashboard_all_storage_title()}</span>
					</div>
					<div class="mt-3 text-3xl font-semibold tracking-tight tabular-nums">{bytes.format(summary.totalImageSize)}</div>
					<div class="text-muted-foreground mt-1 text-sm">{formatStorageOverviewLabel(summary)}</div>
				</div>
			</div>
		{/await}
	</section>

	<section class="flex min-h-0 flex-1 flex-col overflow-hidden">
		<div class="mb-3 flex items-center justify-between gap-3">
			<h2 class="text-lg font-semibold tracking-tight">{m.dashboard_all_environment_board_title()}</h2>
		</div>

		{#if environmentCards.length === 0}
			<div class="border-border/60 rounded-xl border border-dashed px-4 py-8 text-center">
				<p class="text-muted-foreground text-sm">{m.dashboard_all_no_visible_environments()}</p>
			</div>
		{:else}
			<div class="grid grid-cols-1 gap-4 overflow-y-auto pb-2 xl:grid-cols-2">
				{#each environmentCards as item (item.environment.id)}
					{@const baseItem = createBaseEnvironmentOverview(item.environment)}
					{@const environment = baseItem.environment}
					{@const resolvedStatus = resolveEnvironmentStatus(environment)}
					{@const statusVariant = getEnvironmentStatusVariant(resolvedStatus)}
					{@const transportBadge = getTransportBadge(environment)}
					{@const activity = getActivityMeta(environment)}
					{@const isCurrent = currentEnvironmentId === environment.id}
					{@const liveStatsState = getLiveStatsState(environment.id)}
					{@const systemStats = liveStatsState?.stats ?? null}
					{@const liveStatsLoading = liveStatsState?.loading ?? shouldLoadEnvironment(environment)}
					{@const cpuMetric = getCpuMetric(systemStats)}
					{@const memoryMetric = getMemoryMetric(systemStats)}
					{@const diskMetric = getDiskMetric(systemStats)}

					<Card.Root
						variant="outlined"
						class={`overflow-hidden border transition-colors ${isCurrent ? 'border-blue-500/40 bg-blue-500/5' : 'border-border/60'}`}
					>
						<Card.Content class="space-y-4 p-4">
							<div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
								<div class="min-w-0 space-y-2">
									<div class="flex flex-wrap items-center gap-2">
										<div class="text-base font-semibold tracking-tight">{environment.name}</div>
										{#if isCurrent}
											<StatusBadge text={m.common_current()} variant="blue" size="sm" minWidth="none" />
										{/if}
										<StatusBadge
											text={capitalizeFirstLetter(getResolvedStatusLabel(environment))}
											variant={statusVariant}
											size="sm"
											minWidth="none"
										/>
										<StatusBadge text={transportBadge.text} variant={transportBadge.variant} size="sm" minWidth="none" />
										{#await environmentBoardStatePromise then boardState}
											{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
											{@const vInfo =
												loadedItem.versionInfo ||
												(debugUpgrade
													? ({ displayVersion: 'debug', updateAvailable: true, newestVersion: 'debug-v2' } as any)
													: null)}
											{#if vInfo}
												<div class="flex items-center">
													{#if vInfo.updateAvailable || debugUpgrade}
														<ArcaneTooltip.Root>
															<ArcaneTooltip.Trigger
																class="bg-surface/50 text-muted-foreground border-border/50 hover:text-foreground inline-flex items-center rounded-md border px-2 py-[2px] font-mono text-[11px] font-medium transition-colors"
															>
																v{vInfo.displayVersion || vInfo.currentTag || vInfo.currentVersion || 'unknown'}
																<span class="relative ml-1.5 flex h-2 w-2">
																	<span
																		class="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-75"
																	></span>
																	<span class="relative inline-flex h-2 w-2 rounded-full bg-amber-500"></span>
																</span>
															</ArcaneTooltip.Trigger>
															<ArcaneTooltip.Content class="flex flex-col items-start gap-2">
																<span>
																	{m.sidebar_update_available()}{#if vInfo.newestVersion || vInfo.newestDigest}: {vInfo.newestVersion ||
																			vInfo.newestDigest.slice(0, 12)}{/if}
																</span>
																<DashboardEnvironmentUpgradeAction
																	{environment}
																	versionInfo={vInfo}
																	isAdmin={currentUserIsAdmin}
																	debug={debugUpgrade}
																	onRefreshRequested={refreshOverview}
																	render="trigger"
																	bind:open={() => getUpgradeOpen(environment.id), (v) => setUpgradeOpen(environment.id, v)}
																	bind:upgrading={
																		() => getUpgradeUpgrading(environment.id), (v) => setUpgradeUpgrading(environment.id, v)
																	}
																/>
															</ArcaneTooltip.Content>
														</ArcaneTooltip.Root>
													{:else}
														<div
															class="bg-surface/50 text-muted-foreground border-border/50 hover:text-foreground inline-flex items-center rounded-md border px-2 py-[2px] font-mono text-[11px] font-medium transition-colors"
														>
															{vInfo.displayVersion || vInfo.currentTag || vInfo.currentVersion || 'unknown'}
														</div>
													{/if}
												</div>
												{#if vInfo.updateAvailable || debugUpgrade}
													<DashboardEnvironmentUpgradeAction
														{environment}
														versionInfo={vInfo}
														isAdmin={currentUserIsAdmin}
														debug={debugUpgrade}
														onRefreshRequested={refreshOverview}
														render="dialog"
														bind:open={() => getUpgradeOpen(environment.id), (v) => setUpgradeOpen(environment.id, v)}
														bind:upgrading={
															() => getUpgradeUpgrading(environment.id), (v) => setUpgradeUpgrading(environment.id, v)
														}
													/>
												{/if}
											{/if}
										{/await}
									</div>

									<div class="text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
										<span class="font-mono">{environment.apiUrl}</span>
										<span>•</span>
										<span title={activity.title}>{activity.label}: {activity.value}</span>
									</div>
								</div>

								<div class="flex min-w-0 flex-1 items-start justify-end pt-1 sm:pt-0">
									{#await environmentBoardStatePromise}
										<ActionButtonGroup buttons={getEnvironmentActionButtons(baseItem, isCurrent)} size="sm" />
									{:then boardState}
										{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
										<ActionButtonGroup buttons={getEnvironmentActionButtons(loadedItem, isCurrent)} size="sm" />
									{/await}
								</div>
							</div>

							{#if shouldLoadEnvironment(environment) || isEnvironmentOnline(environment)}
								<div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
									<div class="border-border/50 bg-background/50 rounded-lg border p-3">
										<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
											{m.containers_title()}
										</div>
										{#await environmentBoardStatePromise}
											<div class="mt-2 space-y-2">
												<Skeleton class="h-6 w-20" />
												<Skeleton class="h-3 w-24" />
											</div>
										{:then boardState}
											{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
											<div class="mt-1 text-lg font-semibold">
												{loadedItem.containers.runningContainers}/{loadedItem.containers.totalContainers}
											</div>
											<div class="text-muted-foreground text-xs">
												{loadedItem.containers.stoppedContainers}
												{m.common_stopped()}
											</div>
										{/await}
									</div>

									<div class="border-border/50 bg-background/50 rounded-lg border p-3">
										<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">{m.images_title()}</div>
										{#await environmentBoardStatePromise}
											<div class="mt-2 space-y-2">
												<Skeleton class="h-6 w-14" />
												<Skeleton class="h-3 w-28" />
											</div>
										{:then boardState}
											{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
											<div class="mt-1 text-lg font-semibold">{loadedItem.imageUsageCounts.totalImages}</div>
											<div class="text-muted-foreground text-xs">
												{loadedItem.imageUsageCounts.imagesInuse}
												{m.common_in_use()} · {loadedItem.imageUsageCounts.imagesUnused}
												{m.common_unused()}
											</div>
										{/await}
									</div>

									<div class="border-border/50 bg-background/50 rounded-lg border p-3">
										<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
											{m.dashboard_action_items_title()}
										</div>
										{#await environmentBoardStatePromise}
											<div class="mt-2 space-y-2">
												<Skeleton class="h-6 w-12" />
												<Skeleton class="h-3 w-32" />
											</div>
										{:then boardState}
											{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
											<div class="mt-1 text-lg font-semibold">{loadedItem.actionItems.items.length}</div>
											<div class="text-muted-foreground text-xs">{getActionSummary(loadedItem)}</div>
										{/await}
									</div>
								</div>
							{:else}
								<div class="border-border/50 bg-background/50 rounded-lg border px-4 py-3 text-sm">
									<p class="font-medium">{m.dashboard_all_environment_unavailable_title()}</p>
									<p class="text-muted-foreground mt-1">{m.dashboard_all_environment_unavailable_description()}</p>
								</div>
							{/if}

							{#if shouldLoadEnvironment(environment)}
								<div class="border-border/50 bg-background/40 rounded-xl border p-1">
									<div class="grid grid-cols-1 gap-1 sm:grid-cols-3">
										{#if liveStatsLoading}
											{#each [1, 2, 3] as tile (tile)}
												<div class="min-w-0 px-2.5 py-2.5">
													<div class="flex items-start justify-between gap-2">
														<Skeleton class="h-3 w-20" />
														<Skeleton class="h-5 w-12" />
													</div>
													<Skeleton class="mt-2 h-3 w-24" />
													<Skeleton class="mt-3 h-1.5 w-full" />
												</div>
											{/each}
										{:else}
											<DashboardMetricTile
												title={m.dashboard_meter_cpu()}
												icon={CpuIcon}
												value={formatPercent(cpuMetric)}
												label={getCpuMetricLabel(systemStats)}
												meterValue={cpuMetric}
											/>

											<DashboardMetricTile
												title={m.dashboard_meter_memory()}
												icon={MemoryStickIcon}
												value={formatPercent(memoryMetric)}
												label={getCapacityLabel(systemStats?.memoryUsage, systemStats?.memoryTotal)}
												labelClass="truncate"
												meterValue={memoryMetric}
											/>

											<DashboardMetricTile
												title={m.dashboard_meter_disk()}
												icon={VolumesIcon}
												value={formatPercent(diskMetric)}
												label={getCapacityLabel(systemStats?.diskUsage, systemStats?.diskTotal)}
												labelClass="truncate"
												meterValue={diskMetric}
											/>
										{/if}
									</div>
								</div>
							{/if}

							{#await environmentBoardStatePromise then boardState}
								{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
								{#if loadedItem.snapshotState === 'error' && loadedItem.snapshotError}
									<div class="rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-xs text-red-700 dark:text-red-300">
										{m.dashboard_all_summary_unavailable({ error: loadedItem.snapshotError })}
									</div>
								{/if}
							{/await}
						</Card.Content>
					</Card.Root>
				{/each}
			</div>
		{/if}
	</section>
</div>

<PruneConfirmationDialog
	open={isPruneDialogOpen}
	isPruning={!!pruningEnvironmentId}
	onConfirm={confirmPrune}
	onCancel={closePruneDialog}
/>
