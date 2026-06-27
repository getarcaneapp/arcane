<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { formatDistanceToNow } from 'date-fns';
	import { onDestroy, onMount, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { type ActionButton } from '$lib/components/action-button-group/index.js';
	import { cn } from '$lib/utils';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import PruneConfirmationDialog from '$lib/components/dialogs/prune-confirmation-dialog.svelte';
	import DockerInfoDialog from '$lib/components/dialogs/docker-info-dialog.svelte';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import * as Card from '$lib/components/ui/card/index.js';
	import { m } from '$lib/paraglide/messages';
	import { settingsService } from '$lib/services/settings-service';
	import { systemService } from '$lib/services/system-service';
	import { activityStore } from '$lib/stores/activity.store.svelte';
	import { dashboardStore } from '$lib/stores/dashboard.store.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasAnyPermission, hasPermission } from '$lib/utils/auth';
	import type {
		DashboardActionItem,
		DashboardEnvironmentCardState,
		DashboardEnvironmentOverview,
		DashboardOverviewSummary,
		DashboardSnapshot,
		SystemStats
	} from '$lib/types/shared';
	import type { Environment } from '$lib/types/environment';
	import type { DockerInfo } from '$lib/types/docker';
	import type { PruneType, SystemPruneRequest } from '$lib/types/automation';
	import type { Settings } from '$lib/types/settings';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '$lib/utils/api';
	import { capitalizeFirstLetter } from '$lib/utils/formatting';
	import { tryCatch } from '$lib/utils/api';
	import { getEnvironmentStatusVariant, isEnvironmentOnline, resolveEnvironmentStatus } from '$lib/utils/docker';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import { createStatsWebSocket, type ReconnectingWebSocket } from '$lib/utils/ws';
	import { bytes } from '$lib/utils/formatting';
	import {
		ContainersIcon,
		CpuIcon,
		EnvironmentsIcon,
		GpuIcon,
		ImagesIcon,
		InfoIcon,
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
	let pruneDefaults = $state<Settings | null>(null);
	let pruneDefaultsLoadingId = $state<string | null>(null);
	let pruningEnvironmentId = $state<string | null>(null);
	let pendingPruneActivityId = $state<string | null>(null);
	let reloadVersion = $state(0);
	let liveStatsByEnvironmentId = $state<Record<string, EnvironmentLiveStatsState>>({});
	let upgradeDialogOpenById = $state<Record<string, boolean>>({});
	let upgradeDialogUpgradingById = $state<Record<string, boolean>>({});

	let dockerInfoOpen = $state(false);
	let dockerInfoData = $state<DockerInfo | null>(null);
	let dockerInfoPromise = $state<Promise<DockerInfo> | null>(null);
	let dockerInfoError = $state<string | null>(null);
	let dockerInfoByEnvironmentId = $state<Record<string, DockerInfo | undefined>>({});
	let dockerInfoPromiseByEnvironmentId = $state<Record<string, Promise<DockerInfo> | undefined>>({});

	const availableEnvironments = $derived(environmentStore.available);
	const currentEnvironmentId = $derived(environmentStore.selected?.id ?? null);

	function canPruneInEnvironment(envId: string): boolean {
		return hasAnyPermission(['images:prune', 'volumes:prune', 'networks:prune'], envId);
	}
	function canUpgradeEnvironment(): boolean {
		return hasPermission('environments:update');
	}

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

	async function loadDockerInfo(environment: Environment): Promise<DockerInfo> {
		try {
			const info = await systemService.getDockerInfoForEnvironment(environment.id);
			dockerInfoByEnvironmentId[environment.id] = info;
			dockerInfoData = info;
			return info;
		} finally {
			delete dockerInfoPromiseByEnvironmentId[environment.id];
			dockerInfoPromise = null;
		}
	}

	function openDockerInfo(environment: Environment) {
		dockerInfoError = null;
		dockerInfoOpen = true;
		dockerInfoData = dockerInfoByEnvironmentId[environment.id] ?? null;
		if (dockerInfoData) {
			dockerInfoPromise = null;
			return;
		}

		dockerInfoPromise = dockerInfoPromiseByEnvironmentId[environment.id] ?? null;
		if (dockerInfoPromise) {
			return;
		}

		dockerInfoPromise = loadDockerInfo(environment).catch((error) => {
			dockerInfoError = extractApiErrorMessage(error);
			throw error;
		});
		dockerInfoPromiseByEnvironmentId[environment.id] = dockerInfoPromise;
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
	const loadableEnvironmentCards = $derived(environmentCards.filter(({ environment }) => shouldLoadEnvironment(environment)));
	const loadableEnvironmentIds = $derived.by(() => new Set(loadableEnvironmentCards.map(({ environment }) => environment.id)));

	function resolveSnapshotErrorMessage(state: NonNullable<ReturnType<typeof dashboardStore.getEnvironmentState>>): string {
		if (state.errorCode === 'agent_incompatible') {
			return m.dashboard_all_agent_incompatible();
		}
		return state.errorMessage || m.common_unknown();
	}

	const boardState = $derived.by(() => {
		void reloadVersion;

		const overviewById = new Map<string, DashboardEnvironmentOverview>();
		const items: DashboardEnvironmentOverview[] = [];

		for (const { environment } of environmentCards) {
			const state = dashboardStore.getEnvironmentState(environment.id);
			let item: DashboardEnvironmentOverview;

			if (state?.snapshot) {
				// Last-known data keeps rendering even while the environment is
				// erroring; the error banner is shown alongside it.
				const snapshot = state.snapshot;
				item = {
					environment,
					containers: snapshot.containers.counts ?? { runningContainers: 0, stoppedContainers: 0, totalContainers: 0 },
					imageUsageCounts: snapshot.imageUsageCounts,
					actionItems: snapshot.actionItems,
					settings: snapshot.settings,
					versionInfo: snapshot.versionInfo,
					snapshotState: 'ready',
					snapshotError: state.streamError ? resolveSnapshotErrorMessage(state) : undefined
				};
			} else if (state?.streamError) {
				item = {
					...createBaseEnvironmentOverview(environment),
					snapshotState: 'error',
					snapshotError: resolveSnapshotErrorMessage(state)
				};
			} else {
				item = createBaseEnvironmentOverview(environment);
			}

			overviewById.set(environment.id, item);
			items.push(item);
		}

		return {
			overviewById,
			summary: buildOverviewSummaryFromItemsInternal(items)
		};
	});

	function isEnvironmentSnapshotLoading(environmentId: string): boolean {
		return dashboardStore.isSnapshotLoading(environmentId);
	}

	const boardSummaryLoading = $derived.by(() => {
		let hasReachable = false;
		for (const { environment } of environmentCards) {
			if (!shouldLoadEnvironment(environment)) {
				continue;
			}
			hasReachable = true;
			if (dashboardStore.getEnvironmentState(environment.id)?.hasLoaded) {
				return false;
			}
		}
		return hasReachable;
	});

	$effect(() => {
		const environmentsToLoad = loadableEnvironmentCards.map(({ environment }) => environment);

		untrack(() => {
			for (const environment of environmentsToLoad) {
				ensureEnvironmentLiveStats(environment);
			}
		});
	});

	$effect(() => {
		const reachableEnvironmentIds = loadableEnvironmentIds;

		untrack(() => {
			for (const environmentId of Object.keys(liveStatsByEnvironmentId)) {
				if (!reachableEnvironmentIds.has(environmentId)) {
					removeEnvironmentLiveStats(environmentId);
				}
			}
		});
	});

	// A prune runs as a background activity; once the streamed activity reaches a
	// terminal state, refresh so the dashboard reflects the post-prune resource counts.
	// A plain (non-reactive) guard dedupes so the refresh fires once per activity
	// without writing $state inside the effect.
	let refreshedPruneActivityId: string | null = null;
	$effect(() => {
		const id = pendingPruneActivityId;
		if (!id || id === refreshedPruneActivityId) {
			return;
		}

		const status = activityStore.getActivity(id)?.status;
		if (status === 'success' || status === 'failed' || status === 'cancelled') {
			refreshedPruneActivityId = id;
			void refreshOverview();
		}
	});

	onMount(() => {
		void dashboardStore.start({ debugAllGood });
	});

	onDestroy(() => {
		cleanupEnvironmentLiveStats();
		dashboardStore.stop();
	});

	async function refreshOverview() {
		isRefreshing = true;
		try {
			await invalidateAll();
			await dashboardStore.refresh();
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

		// Prefer the live tunnel transport; fall back to the last one used so
		// disconnected or poll-only agents still show what they connect with.
		const transport = (environment.connected ? environment.edgeTransport : undefined) ?? environment.lastEdgeTransport;
		if (!transport) {
			return { text: m.dashboard_all_transport_edge(), variant: 'gray' };
		}

		return transport === 'websocket'
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

	function getGpuMetric(stats: SystemStats | null): number | null {
		const gpus = stats?.gpus?.filter((gpu) => gpu.memoryTotal > 0) ?? [];
		if (gpus.length === 0) return null;
		const totalPercent = gpus.reduce((sum, gpu) => sum + (gpu.memoryUsed / gpu.memoryTotal) * 100, 0);
		return totalPercent / gpus.length;
	}

	function getGpuMetricLabel(stats: SystemStats | null): string {
		const count = stats?.gpuCount ?? 0;
		return count > 0 ? `${count} ${count === 1 ? m.dashboard_meter_gpu_device() : m.dashboard_meter_gpu_devices()}` : '--';
	}

	function canPruneEnvironment(item: DashboardEnvironmentOverview): boolean {
		return (
			canPruneInEnvironment(item.environment.id) &&
			item.environment.enabled &&
			item.snapshotState === 'ready' &&
			isEnvironmentOnline(item.environment)
		);
	}

	function getEnvironmentActionButtons(item: DashboardEnvironmentOverview, isCurrent: boolean): ActionButton[] {
		const buttons: ActionButton[] = [];

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
			onclick: () => void goto(resolve(`/environments/${item.environment.id}`)),
			icon: InspectIcon
		});

		buttons.push({
			id: `${item.environment.id}-docker-info`,
			action: 'base',
			label: m.common_inspect(),
			disabled: !shouldLoadEnvironment(item.environment),
			onclick: () => openDockerInfo(item.environment),
			icon: InfoIcon
		});

		if (canPruneInEnvironment(item.environment.id)) {
			buttons.push({
				id: `${item.environment.id}-prune`,
				action: 'prune',
				label: m.quick_actions_prune_system(),
				loading: pruningEnvironmentId === item.environment.id || pruneDefaultsLoadingId === item.environment.id,
				disabled: !canPruneEnvironment(item) || !!pruningEnvironmentId || !!pruneDefaultsLoadingId,
				onclick: () => void openPruneDialog(item),
				icon: TrashIcon
			});
		}

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

	async function openPruneDialog(item: DashboardEnvironmentOverview) {
		if (!canPruneEnvironment(item) || pruneDefaultsLoadingId) {
			return;
		}

		const environmentId = item.environment.id;
		pruneEnvironment = item;
		pruneDefaultsLoadingId = environmentId;
		try {
			// Pre-fill the dialog with this environment's configured prune defaults.
			pruneDefaults = await settingsService.getSettingsForEnvironment(environmentId);
		} catch {
			// Fall back to the dialog's built-in defaults if settings can't be loaded.
			pruneDefaults = null;
		} finally {
			pruneDefaultsLoadingId = null;
		}

		// Guard against the selection changing while the fetch was in flight.
		if (pruneEnvironment?.environment.id === environmentId) {
			isPruneDialogOpen = true;
		}
	}

	function closePruneDialog() {
		if (pruningEnvironmentId) {
			return;
		}

		isPruneDialogOpen = false;
		pruneEnvironment = null;
		pruneDefaults = null;
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
			onSuccess: async (data) => {
				isPruneDialogOpen = false;
				pruneEnvironment = null;
				pruneDefaults = null;
				const activityId = extractActivityId(data);
				const toastOptions = {
					...(activityToastOptions(activityId) ?? {}),
					description: targetEnvironment.environment.name
				};
				if (selectedTypes.length === 1) {
					toast.success(m.dashboard_prune_success_one({ types: typesString }), toastOptions);
				} else {
					toast.success(m.dashboard_prune_success_many({ types: typesString }), toastOptions);
				}
				// The prune runs as a background activity, so refresh once it actually
				// completes — refreshing now would capture pre-prune state. Fall back to
				// an immediate refresh when no activity id is returned.
				if (activityId) {
					pendingPruneActivityId = activityId;
				} else {
					await refreshOverview();
				}
			}
		});
	}
</script>

<div class="flex h-full min-h-0 flex-col gap-3 overflow-hidden pt-2 md:gap-4 md:pt-3">
	<header class="bg-card/60 backdrop-blur-md border-border/70 shrink-0 rounded-xl border p-3 shadow-xs sm:p-4">
		<div class="relative flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
			<div class="space-y-1">
				<p class="text-muted-foreground text-[11px] font-semibold tracking-[0.14em] uppercase">{m.dashboard_title()}</p>
				<h1 class="text-xl font-semibold tracking-tight sm:text-2xl">{heroGreeting}</h1>
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

	<section class="shrink-0 space-y-2">
		<h2 class="text-base font-semibold tracking-tight">{m.common_overview()}</h2>

		{#if boardSummaryLoading}
			<div class="grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-4">
				{#each [{ icon: EnvironmentsIcon, label: m.environments_title() }, { icon: ContainersIcon, label: m.containers_title() }, { icon: ImagesIcon, label: m.images_title() }, { icon: VolumesIcon, label: m.dashboard_all_storage_title() }] as tile (tile.label)}
					<div class="border-border/50 bg-background/50 rounded-xl border p-3">
						<div class="text-muted-foreground flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
							<tile.icon class="size-3.5" />
							<span>{tile.label}</span>
						</div>
						<Skeleton class="mt-2 h-7 w-12" />
						<Skeleton class="mt-1.5 h-3.5 w-28" />
					</div>
				{/each}
			</div>
		{:else}
			{@const summary = boardState.summary}
			<div class="grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-4">
				<div class="border-border/50 bg-background/50 rounded-xl border p-3">
					<div class="text-muted-foreground flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
						<EnvironmentsIcon class="size-3.5" />
						<span>{m.environments_title()}</span>
					</div>
					<div class="mt-2 text-2xl font-semibold tracking-tight tabular-nums">{summary.totalEnvironments}</div>
					<div class="text-muted-foreground mt-0.5 text-xs">{formatEnvironmentOverviewLabel(summary)}</div>
				</div>

				<div class="border-border/50 bg-background/50 rounded-xl border p-3">
					<div class="text-muted-foreground flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
						<ContainersIcon class="size-3.5" />
						<span>{m.containers_title()}</span>
					</div>
					<div class="mt-2 text-2xl font-semibold tracking-tight tabular-nums">{summary.totalContainers}</div>
					<div class="text-muted-foreground mt-0.5 text-xs">{formatContainerOverviewLabel(summary)}</div>
				</div>

				<div class="border-border/50 bg-background/50 rounded-xl border p-3">
					<div class="text-muted-foreground flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
						<ImagesIcon class="size-3.5" />
						<span>{m.images_title()}</span>
					</div>
					<div class="mt-2 text-2xl font-semibold tracking-tight tabular-nums">{summary.totalImages}</div>
					<div class="text-muted-foreground mt-0.5 text-xs">{formatImageOverviewLabel(summary)}</div>
				</div>

				<div class="border-border/50 bg-background/50 rounded-xl border p-3">
					<div class="text-muted-foreground flex items-center gap-1.5 text-[11px] font-semibold tracking-wide uppercase">
						<VolumesIcon class="size-3.5" />
						<span>{m.dashboard_all_storage_title()}</span>
					</div>
					<div class="mt-2 text-2xl font-semibold tracking-tight tabular-nums">{bytes.format(summary.totalImageSize)}</div>
					<div class="text-muted-foreground mt-0.5 text-xs">{formatStorageOverviewLabel(summary)}</div>
				</div>
			</div>
		{/if}
	</section>

	<section class="flex min-h-0 flex-1 flex-col overflow-hidden">
		<div class="mb-2 flex shrink-0 items-center justify-between gap-3">
			<h2 class="text-base font-semibold tracking-tight">{m.dashboard_all_environment_board_title()}</h2>
		</div>

		{#if environmentCards.length === 0}
			<div class="border-border/60 rounded-xl border border-dashed px-4 py-8 text-center">
				<p class="text-muted-foreground text-sm">{m.dashboard_all_no_visible_environments()}</p>
			</div>
		{:else}
			<div class="min-h-0 flex-1 overflow-y-auto pb-2">
				<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
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
						{@const gpuMetric = getGpuMetric(systemStats)}

						<Card.Root
							variant="outlined"
							class={`dashboard-environment-card [container-type:inline-size] overflow-hidden border transition-colors ${isCurrent ? 'border-blue-500/40 bg-blue-500/5' : 'border-border/60'}`}
						>
							<Card.Content class="space-y-5 p-5">
								<div class="border-border/60 flex flex-col gap-3 border-b pb-4 sm:flex-row sm:items-start sm:justify-between">
									<div class="min-w-0 space-y-2">
										<div class="flex min-w-0 flex-wrap items-center gap-2">
											<div class="min-w-0 max-w-full break-words text-base font-semibold tracking-tight">{environment.name}</div>
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
											{#if boardState}
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
																		canUpgrade={canUpgradeEnvironment()}
																		debug={debugUpgrade}
																		onRefreshRequested={refreshOverview}
																		render="trigger"
																		bind:open={upgradeDialogOpenById[environment.id]}
																		bind:upgrading={upgradeDialogUpgradingById[environment.id]}
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
															canUpgrade={canUpgradeEnvironment()}
															debug={debugUpgrade}
															onRefreshRequested={refreshOverview}
															render="dialog"
															bind:open={upgradeDialogOpenById[environment.id]}
															bind:upgrading={upgradeDialogUpgradingById[environment.id]}
														/>
													{/if}
												{/if}
											{/if}
										</div>

										<div class="text-muted-foreground/70 mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px]">
											<span class="font-mono">{environment.apiUrl}</span>
											<span>•</span>
											<span title={activity.title}>{activity.label}: {activity.value}</span>
										</div>
									</div>

									<div class="flex shrink-0 items-center gap-1 pt-1 sm:pt-0">
										{#each getEnvironmentActionButtons(boardState.overviewById.get(environment.id) ?? baseItem, isCurrent) as btn (btn.id)}
											{@const isActiveEnv = isCurrent && btn.id === `${environment.id}-use`}
											<ArcaneTooltip.Root>
												<ArcaneTooltip.Trigger>
													{#snippet child({ props })}
														<ArcaneButton
															{...props}
															action={btn.action}
															size="icon"
															tone="ghost"
															icon={btn.icon}
															customLabel={btn.label}
															loading={btn.loading}
															disabled={btn.disabled}
															onclick={btn.onclick}
															class={cn(
																'size-8',
																btn.action === 'prune' && 'text-destructive hover:bg-destructive/10 hover:text-destructive',
																isActiveEnv && 'disabled:opacity-100 [&_svg]:text-blue-600! dark:[&_svg]:text-blue-500!'
															)}
														/>
													{/snippet}
												</ArcaneTooltip.Trigger>
												<ArcaneTooltip.Content>{isActiveEnv ? m.common_current() : btn.label}</ArcaneTooltip.Content>
											</ArcaneTooltip.Root>
										{/each}
									</div>
								</div>

								{#if shouldLoadEnvironment(environment) || isEnvironmentOnline(environment)}
									<div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
										<div class="border-border/50 bg-background/50 rounded-lg border p-3">
											<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
												{m.containers_title()}
											</div>
											{#if isEnvironmentSnapshotLoading(environment.id)}
												<div class="mt-2 space-y-2">
													<Skeleton class="h-6 w-20" />
													<Skeleton class="h-3 w-24" />
												</div>
											{:else}
												{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
												<div class="mt-1 text-lg font-semibold">
													{loadedItem.containers.runningContainers}/{loadedItem.containers.totalContainers}
												</div>
												<div class="text-muted-foreground text-xs">
													{loadedItem.containers.stoppedContainers}
													{m.common_stopped()}
												</div>
											{/if}
										</div>

										<div class="border-border/50 bg-background/50 rounded-lg border p-3">
											<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
												{m.images_title()}
											</div>
											{#if isEnvironmentSnapshotLoading(environment.id)}
												<div class="mt-2 space-y-2">
													<Skeleton class="h-6 w-14" />
													<Skeleton class="h-3 w-28" />
												</div>
											{:else}
												{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
												<div class="mt-1 text-lg font-semibold">{loadedItem.imageUsageCounts.totalImages}</div>
												<div class="text-muted-foreground text-xs">
													{loadedItem.imageUsageCounts.imagesInuse}
													{m.common_in_use()} · {loadedItem.imageUsageCounts.imagesUnused}
													{m.common_unused()}
												</div>
											{/if}
										</div>

										<div class="border-border/50 bg-background/50 rounded-lg border p-3">
											<div class="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
												{m.dashboard_action_items_title()}
											</div>
											{#if isEnvironmentSnapshotLoading(environment.id)}
												<div class="mt-2 space-y-2">
													<Skeleton class="h-6 w-12" />
													<Skeleton class="h-3 w-32" />
												</div>
											{:else}
												{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
												<div class="mt-1 text-lg font-semibold">{loadedItem.actionItems.items.length}</div>
												<div class="text-muted-foreground text-xs">{getActionSummary(loadedItem)}</div>
											{/if}
										</div>
									</div>
								{:else}
									<div class="border-border/50 bg-background/50 rounded-lg border px-4 py-3 text-sm">
										<p class="font-medium">{m.dashboard_all_environment_unavailable_title()}</p>
										<p class="text-muted-foreground mt-1">{m.dashboard_all_environment_unavailable_description()}</p>
									</div>
								{/if}

								{#if shouldLoadEnvironment(environment)}
									<div class="pt-1">
										<div class="grid grid-cols-1 gap-1 {gpuMetric !== null ? 'sm:grid-cols-2 lg:grid-cols-4' : 'sm:grid-cols-3'}">
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

												{#if gpuMetric !== null}
													<DashboardMetricTile
														title={m.dashboard_meter_gpu()}
														icon={GpuIcon}
														value={formatPercent(gpuMetric)}
														label={getGpuMetricLabel(systemStats)}
														meterValue={gpuMetric}
													/>
												{/if}
											{/if}
										</div>
									</div>
								{/if}

								{#if boardState}
									{@const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem}
									{#if loadedItem.snapshotError}
										<div
											class="rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-xs text-red-700 dark:text-red-300"
										>
											{m.dashboard_all_summary_unavailable({ error: loadedItem.snapshotError })}
										</div>
									{/if}
								{/if}
							</Card.Content>
						</Card.Root>
					{/each}
				</div>
			</div>
		{/if}
	</section>
</div>

<PruneConfirmationDialog
	open={isPruneDialogOpen}
	isPruning={!!pruningEnvironmentId}
	defaults={pruneDefaults}
	onConfirm={confirmPrune}
	onCancel={closePruneDialog}
/>

<DockerInfoDialog bind:open={dockerInfoOpen} dockerInfo={dockerInfoData} {dockerInfoPromise} errorMessage={dockerInfoError} />
