<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, keepPreviousData } from '@tanstack/svelte-query';
	import { PersistedState } from 'runed';
	import { onDestroy, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { type ActionButton } from '#lib/components/action-button-group/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import DockerInfoDialog, { type EngineInfo } from '#lib/components/dialogs/docker-info-dialog.svelte';
	import PruneConfirmationDialog from '#lib/components/dialogs/prune-confirmation-dialog.svelte';
	import * as StatStrip from '#lib/components/stat-strip/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import {
		AlertTriangleIcon,
		BoxIcon,
		ContainersIcon,
		ImagesIcon,
		InfoIcon,
		InspectIcon,
		TrashIcon,
		UpdateIcon,
		VolumesIcon,
		LayoutGridIcon,
		LayoutListIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { apiKeyService } from '#lib/services/api-key-service.js';
	import { containerService } from '#lib/services/container-service.js';
	import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
	import { imageService } from '#lib/services/image-service.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import { systemService } from '#lib/services/system-service.js';
	import { volumeService } from '#lib/services/volume-service.js';
	import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { PruneType, SystemPruneRequest } from '#lib/types/automation.js';
	import type { ContainerStatusCounts, ImageUsageCounts, VolumeUsageCounts } from '#lib/types/docker.js';
	import type { Environment, EnvironmentVulnerabilitySummary } from '#lib/types/environment.js';
	import type { PodmanStatus } from '#lib/types/podman.js';
	import type { AppVersionInformation, Settings } from '#lib/types/settings.js';
	import type {
		DashboardEnvironmentCardState,
		DashboardEnvironmentOverview,
		DashboardLiveStatsStatus,
		SystemStats
	} from '#lib/types/shared.js';
	import type { ProjectStatusCounts } from '#lib/types/swarm.js';
	import { cn } from '#lib/utils.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasAnyPermission, hasPermission } from '#lib/utils/auth.js';
	import { isEnvironmentOnline } from '#lib/utils/docker.js';
	import { readableEnvironments, sumAcrossEnvironments, type EnvironmentFailure } from '#lib/utils/environments.js';
	import { parseInstant, withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { createStatsWebSocket, type ReconnectingWebSocket } from '#lib/utils/ws.js';

	import {
		buildOverviewSummary,
		createBaseEnvironmentOverview,
		formatContainerOverviewLabel,
		formatImageOverviewLabel,
		formatVolumeOverviewLabel,
		shouldLoadEnvironment
	} from '../overview.helpers';
	import EnvironmentCard from './environment-card.svelte';
	import EnvironmentsTable, { type EnvironmentTableRow } from './environments-table.svelte';

	let {
		heroGreeting,
		debugUpgrade = false
	}: {
		heroGreeting: string;
		debugUpgrade?: boolean;
	} = $props();

	const DEBUG_VERSION_INFO: AppVersionInformation = {
		currentVersion: 'debug',
		displayVersion: 'debug',
		revision: 'debug',
		shortRevision: 'debug',
		goVersion: '',
		nodeVersion: '',
		svelteKitVersion: '',
		isSemverVersion: false,
		newestVersion: 'debug-v2',
		updateAvailable: true
	};

	const EMPTY_CONTAINER_COUNTS: ContainerStatusCounts = {
		runningContainers: 0,
		stoppedContainers: 0,
		totalContainers: 0,
		updatesAvailable: 0
	};
	const EMPTY_IMAGE_COUNTS: ImageUsageCounts = {
		imagesInuse: 0,
		imagesUnused: 0,
		totalImages: 0,
		totalImageSize: 0
	};
	const EMPTY_VOLUME_COUNTS: VolumeUsageCounts = {
		inuse: 0,
		unused: 0,
		total: 0
	};
	const ALL_API_KEYS = { pagination: { page: 1, limit: -1 } };
	const API_KEY_EXPIRY_WINDOW_HOURS = 14 * 24;

	type EnvironmentLiveStatsState = {
		stats: SystemStats | null;
		loading: boolean;
		failed: boolean;
		client: ReconnectingWebSocket<SystemStats> | null;
		deadline: ReturnType<typeof setTimeout> | null;
	};

	const LIVE_STATS_DEADLINE_MS = 10_000;

	let isRefreshing = $state(false);
	let isPruneDialogOpen = $state(false);
	let pruneEnvironment = $state<DashboardEnvironmentOverview | null>(null);
	let pruneDefaults = $state<Settings | null>(null);
	let pruneDefaultsLoadingId = $state<string | null>(null);
	let pruningEnvironmentId = $state<string | null>(null);
	let liveStatsByEnvironmentId = $state<Record<string, EnvironmentLiveStatsState>>({});
	let upgradeDialogOpenById = $state<Record<string, boolean>>({});
	let upgradeDialogUpgradingById = $state<Record<string, boolean>>({});

	let engineInfoOpen = $state(false);
	let engineInfoEngine = $state<EngineInfo['engine']>('docker');
	let engineInfoData = $state<EngineInfo | null>(null);
	let engineInfoPromise = $state<Promise<EngineInfo> | null>(null);
	let engineInfoError = $state<string | null>(null);
	let engineInfoByEnvironmentId = $state<Record<string, EngineInfo | undefined>>({});
	let engineInfoPromiseByEnvironmentId = $state<Record<string, Promise<EngineInfo> | undefined>>({});

	const availableEnvironments = $derived.by(() => {
		if (!userStore.current) {
			return [];
		}

		return environmentStore.available.filter(
			(environment) => environment.enabled && hasPermission('dashboard:read', environment.id)
		);
	});

	function canPruneInEnvironment(envId: string): boolean {
		return hasAnyPermission(['images:prune', 'volumes:prune', 'networks:prune'], envId);
	}
	function canUpgradeEnvironment(envId: string): boolean {
		return hasPermission('environments:update', envId);
	}

	function createEmptyLiveStatsState(): EnvironmentLiveStatsState {
		return {
			stats: null,
			loading: true,
			failed: false,
			client: null,
			deadline: null
		};
	}

	function canLoadLiveStats(environment: Environment): boolean {
		return shouldLoadEnvironment(environment) && hasPermission('system:read', environment.id);
	}

	function clearLiveStatsDeadline(liveStatsState: EnvironmentLiveStatsState) {
		if (liveStatsState.deadline) {
			clearTimeout(liveStatsState.deadline);
			liveStatsState.deadline = null;
		}
	}

	function markLiveStatsFailed(liveStatsState: EnvironmentLiveStatsState) {
		clearLiveStatsDeadline(liveStatsState);
		liveStatsState.loading = false;
		liveStatsState.failed = true;
	}

	function startLiveStatsAttempt(liveStatsState: EnvironmentLiveStatsState) {
		clearLiveStatsDeadline(liveStatsState);
		liveStatsState.loading = liveStatsState.stats === null;
		liveStatsState.failed = liveStatsState.stats !== null && liveStatsState.failed;
		liveStatsState.deadline = setTimeout(() => markLiveStatsFailed(liveStatsState), LIVE_STATS_DEADLINE_MS);
	}

	function ensureEnvironmentLiveStats(environment: Environment) {
		if (!canLoadLiveStats(environment)) {
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

		startLiveStatsAttempt(liveStatsState);
		liveStatsState.client = createStatsWebSocket({
			getEnvId: () => environment.id,
			onMessage: (stats) => {
				clearLiveStatsDeadline(liveStatsState);
				liveStatsState.stats = stats;
				liveStatsState.loading = false;
				liveStatsState.failed = false;
			},
			onError: (error) => {
				console.error(`Stats websocket error for environment ${environment.id}:`, error);
				markLiveStatsFailed(liveStatsState);
			},
			onClose: () => markLiveStatsFailed(liveStatsState)
		});
		liveStatsState.client.connect();
	}

	function retryFailedLiveStats() {
		for (const liveStatsState of Object.values(liveStatsByEnvironmentId)) {
			if (!liveStatsState.failed || !liveStatsState.client) {
				continue;
			}
			startLiveStatsAttempt(liveStatsState);
			void liveStatsState.client.connect();
		}
	}

	function removeEnvironmentLiveStats(environmentId: string) {
		const liveStatsState = liveStatsByEnvironmentId[environmentId];
		if (!liveStatsState) {
			return;
		}

		clearLiveStatsDeadline(liveStatsState);
		liveStatsState.client?.close();
		delete liveStatsByEnvironmentId[environmentId];
	}

	function cleanupEnvironmentLiveStats() {
		for (const environmentId of Object.keys(liveStatsByEnvironmentId)) {
			removeEnvironmentLiveStats(environmentId);
		}
	}

	async function fetchEngineInfo(environmentId: string, podCount: number | undefined): Promise<EngineInfo> {
		if (podCount === undefined) {
			return { engine: 'docker', docker: await systemService.getDockerInfoForEnvironment(environmentId) };
		}

		const [podman, status] = await Promise.all([podmanService.getInfo(environmentId), podmanService.getStatus(environmentId)]);
		return { engine: 'podman', podman, status, podCount };
	}

	async function loadEngineInfo(environmentId: string, podCount: number | undefined): Promise<EngineInfo> {
		try {
			const info = await fetchEngineInfo(environmentId, podCount);
			engineInfoByEnvironmentId[environmentId] = info;
			engineInfoData = info;
			return info;
		} finally {
			delete engineInfoPromiseByEnvironmentId[environmentId];
			engineInfoPromise = null;
		}
	}

	function openEngineInfo(item: DashboardEnvironmentOverview) {
		const environmentId = item.environment.id;
		const podCount = hasPermission('podman:read', environmentId) ? item.podCount : undefined;
		engineInfoError = null;
		engineInfoOpen = true;
		engineInfoEngine = podCount === undefined ? 'docker' : 'podman';
		engineInfoData = engineInfoByEnvironmentId[environmentId] ?? null;
		if (engineInfoData) {
			engineInfoPromise = null;
			return;
		}

		engineInfoPromise = engineInfoPromiseByEnvironmentId[environmentId] ?? null;
		if (engineInfoPromise) {
			return;
		}

		engineInfoPromise = tryCatch(loadEngineInfo(environmentId, podCount)).then((result) => {
			if (result.error) {
				const error: Error = result.error;

				engineInfoError = extractApiErrorMessage(error);
				throw error;
			}
			return result.data;
		});
		engineInfoPromiseByEnvironmentId[environmentId] = engineInfoPromise;
	}

	const environmentCards = $derived.by((): DashboardEnvironmentCardState[] => {
		return [...availableEnvironments]
			.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.id.localeCompare(b.id))
			.map((environment) => ({ environment }));
	});
	const loadableEnvironmentCards = $derived(environmentCards.filter(({ environment }) => shouldLoadEnvironment(environment)));
	const loadableEnvironmentIds = $derived(new Set(loadableEnvironmentCards.map(({ environment }) => environment.id)));
	const liveStatsEnvironmentIds = $derived.by(
		() =>
			new Set(
				loadableEnvironmentCards
					.filter(({ environment }) => canLoadLiveStats(environment))
					.map(({ environment }) => environment.id)
			)
	);

	function loadableEnvironments(permission: string): EnvironmentRef[] {
		return readableEnvironments(permission).filter((environment) => loadableEnvironmentIds.has(environment.id));
	}

	function dashboardQueryKey(domain: string, environments: EnvironmentRef[]) {
		return [domain, environmentStore.shownKey, 'dashboard', environments.map((environment) => environment.id).join(',')];
	}

	function byEnvironmentId<C>(result: { byEnvironment: { environment: EnvironmentRef; data: C }[] } | undefined): Map<string, C> {
		return new Map((result?.byEnvironment ?? []).map(({ environment, data }) => [environment.id, data]));
	}

	function isVersionInformation(value: Partial<AppVersionInformation> | undefined): value is AppVersionInformation {
		return value?.currentVersion !== undefined;
	}

	const containersQuery = createQuery(() => {
		const environments = loadableEnvironments('containers:list');
		return {
			queryKey: dashboardQueryKey('containers', environments),
			queryFn: () =>
				sumAcrossEnvironments(
					environments,
					(scope, config) => containerService.getContainerStatusCountsForEnvironment(scope, config),
					EMPTY_CONTAINER_COUNTS
				),
			placeholderData: keepPreviousData
		};
	});

	const projectsQuery = createQuery(() => {
		const environments = loadableEnvironments('projects:list');
		return {
			queryKey: dashboardQueryKey('projects', environments),
			queryFn: () =>
				sumAcrossEnvironments<Pick<ProjectStatusCounts, 'updatesAvailable'>>(
					environments,
					(scope, config) => projectService.getProjectStatusCountsForEnvironment(scope, config),
					{ updatesAvailable: 0 }
				),
			placeholderData: keepPreviousData
		};
	});

	const imagesQuery = createQuery(() => {
		const environments = loadableEnvironments('images:list');
		return {
			queryKey: dashboardQueryKey('images', environments),
			queryFn: () =>
				sumAcrossEnvironments(
					environments,
					(scope, config) => imageService.getImageUsageCountsForEnvironment(scope, config),
					EMPTY_IMAGE_COUNTS
				),
			placeholderData: keepPreviousData
		};
	});

	const volumesQuery = createQuery(() => {
		const environments = loadableEnvironments('volumes:list');
		return {
			queryKey: dashboardQueryKey('volumes', environments),
			queryFn: () =>
				sumAcrossEnvironments(
					environments,
					(scope, config) => volumeService.getVolumeUsageCounts(scope, config),
					EMPTY_VOLUME_COUNTS
				),
			placeholderData: keepPreviousData
		};
	});

	const vulnerabilitiesQuery = createQuery(() => {
		const environments = loadableEnvironments('vulnerabilities:read').filter((environment) =>
			featureStore.isEnabled('vulnerabilityManagement', environment.id)
		);
		return {
			queryKey: dashboardQueryKey('vulnerabilities', environments),
			queryFn: () =>
				sumAcrossEnvironments<Pick<EnvironmentVulnerabilitySummary, 'actionableCount'>>(
					environments,
					(scope, config) => vulnerabilityService.getEnvironmentSummary(scope, config),
					{ actionableCount: 0 }
				),
			placeholderData: keepPreviousData
		};
	});

	const podmanQuery = createQuery(() => {
		const environments = loadableEnvironments('podman:read');
		return {
			queryKey: dashboardQueryKey('podman', environments),
			queryFn: () =>
				sumAcrossEnvironments<Pick<PodmanStatus, 'detected' | 'podCount'>>(
					environments,
					(scope, config) => podmanService.getStatus(scope, config),
					{ detected: false, podCount: 0 }
				),
			placeholderData: keepPreviousData
		};
	});

	const versionsQuery = createQuery(() => {
		const environments = loadableEnvironments('environments:read');
		return {
			queryKey: dashboardQueryKey('environments', environments),
			queryFn: () =>
				sumAcrossEnvironments<Partial<AppVersionInformation>>(
					environments,
					(scope, config) => environmentManagementService.getVersion(scope, config),
					{}
				),
			placeholderData: keepPreviousData
		};
	});

	const apiKeysQuery = createQuery(() => ({
		queryKey: queryKeys.apiKeys.list(ALL_API_KEYS),
		queryFn: () => apiKeyService.getApiKeys(ALL_API_KEYS),
		enabled: userStore.hasPermission('apikeys:list')
	}));

	const environmentQueries = [
		containersQuery,
		projectsQuery,
		imagesQuery,
		volumesQuery,
		vulnerabilitiesQuery,
		podmanQuery,
		versionsQuery
	];

	const containersByEnvironment = $derived(byEnvironmentId(containersQuery.data));
	const projectsByEnvironment = $derived(byEnvironmentId(projectsQuery.data));
	const imagesByEnvironment = $derived(byEnvironmentId(imagesQuery.data));
	const vulnerabilitiesByEnvironment = $derived(byEnvironmentId(vulnerabilitiesQuery.data));
	const podmanByEnvironment = $derived(byEnvironmentId(podmanQuery.data));
	const versionsByEnvironment = $derived(byEnvironmentId(versionsQuery.data));

	const failedEnvironments = $derived.by((): EnvironmentFailure[] => {
		const failures = environmentQueries.flatMap((query) => query.data?.failedEnvironments ?? []);
		return failures.filter(
			(failure, index) => failures.findIndex((other) => other.environment.id === failure.environment.id) === index
		);
	});
	const failedEnvironmentMessages = $derived.by(() => {
		const messages = new Map<string, string[]>();
		for (const query of environmentQueries) {
			for (const { environment, message } of query.data?.failedEnvironments ?? []) {
				messages.set(environment.id, [...(messages.get(environment.id) ?? []), message]);
			}
		}
		return messages;
	});
	const queryErrors = $derived(
		[...environmentQueries, apiKeysQuery].flatMap((query) => (query.error ? [extractApiErrorMessage(query.error)] : []))
	);
	const excludedEnvironmentCount = $derived(
		Math.max(0, ...environmentQueries.map((query) => query.data?.excludedEnvironments.length ?? 0))
	);

	const expiringApiKeys = $derived.by(() => {
		const cutoff = Temporal.Now.instant().add({ hours: API_KEY_EXPIRY_WINDOW_HOURS });
		return (apiKeysQuery.data?.data ?? []).filter((apiKey) => {
			const expiresAt = parseInstant(apiKey.expiresAt);
			return expiresAt !== null && Temporal.Instant.compare(expiresAt, cutoff) <= 0;
		}).length;
	});

	function buildEnvironmentOverview(environment: Environment): DashboardEnvironmentOverview {
		const containers = containersByEnvironment.get(environment.id) ?? EMPTY_CONTAINER_COUNTS;
		const updates = containers.updatesAvailable + (projectsByEnvironment.get(environment.id)?.updatesAvailable ?? 0);
		const vulnerabilities = vulnerabilitiesByEnvironment.get(environment.id)?.actionableCount ?? 0;
		const expiringKeys = environment.id === LOCAL_DOCKER_ENVIRONMENT_ID ? expiringApiKeys : 0;
		const podman = podmanByEnvironment.get(environment.id);
		const versionInfo = versionsByEnvironment.get(environment.id);

		return {
			environment,
			containers,
			imageUsageCounts: imagesByEnvironment.get(environment.id) ?? EMPTY_IMAGE_COUNTS,
			updates,
			vulnerabilities,
			actionItemCount: [containers.stoppedContainers, updates, vulnerabilities, expiringKeys].filter((count) => count > 0).length,
			versionInfo: isVersionInformation(versionInfo) ? versionInfo : undefined,
			podCount: podman?.detected ? podman.podCount : undefined,
			error: failedEnvironmentMessages.get(environment.id)?.join('; ')
		};
	}

	const overviewById = $derived(
		new Map(environmentCards.map(({ environment }) => [environment.id, buildEnvironmentOverview(environment)]))
	);

	const boardSummary = $derived(
		buildOverviewSummary([...overviewById.values()], {
			containers: containersQuery.data ?? EMPTY_CONTAINER_COUNTS,
			images: imagesQuery.data ?? EMPTY_IMAGE_COUNTS,
			volumes: volumesQuery.data ?? EMPTY_VOLUME_COUNTS,
			pods: podmanQuery.data?.podCount ?? 0
		})
	);

	function isEnvironmentCountsLoading(environmentId: string): boolean {
		return loadableEnvironmentIds.has(environmentId) && !containersByEnvironment.has(environmentId) && containersQuery.isFetching;
	}

	const boardSummaryLoading = $derived(loadableEnvironmentIds.size > 0 && containersQuery.isPending);

	$effect(() => {
		const environmentsToLoad = loadableEnvironmentCards.map(({ environment }) => environment);

		untrack(() => {
			for (const environment of environmentsToLoad) {
				ensureEnvironmentLiveStats(environment);
				void featureStore.load(environment.id);
			}
		});
	});

	$effect(() => {
		const activeEnvironmentIds = liveStatsEnvironmentIds;

		untrack(() => {
			for (const environmentId of Object.keys(liveStatsByEnvironmentId)) {
				if (!activeEnvironmentIds.has(environmentId)) {
					removeEnvironmentLiveStats(environmentId);
				}
			}
		});
	});

	onDestroy(() => {
		cleanupEnvironmentLiveStats();
	});

	async function refetchEnvironmentQueries() {
		await Promise.all([...environmentQueries, apiKeysQuery].map((query) => query.refetch()));
	}

	async function refreshOverview() {
		isRefreshing = true;
		try {
			retryFailedLiveStats();
			await refetchEnvironmentQueries();
		} finally {
			isRefreshing = false;
		}
	}

	function getLiveStatsState(environmentId: string): EnvironmentLiveStatsState | null {
		return liveStatsByEnvironmentId[environmentId] ?? null;
	}

	function getLiveStatsStatus(environment: Environment): DashboardLiveStatsStatus {
		if (!shouldLoadEnvironment(environment)) {
			return 'live';
		}
		if (!hasPermission('system:read', environment.id)) {
			return 'denied';
		}
		const liveStatsState = getLiveStatsState(environment.id);
		if (!liveStatsState) {
			return 'loading';
		}
		if (liveStatsState.failed) {
			return liveStatsState.stats ? 'stale' : 'unavailable';
		}
		return liveStatsState.loading ? 'loading' : 'live';
	}

	function canPruneEnvironment(item: DashboardEnvironmentOverview): boolean {
		return (
			canPruneInEnvironment(item.environment.id) &&
			item.environment.enabled &&
			item.error === undefined &&
			isEnvironmentOnline(item.environment)
		);
	}

	function getEnvironmentActionButtons(item: DashboardEnvironmentOverview): ActionButton[] {
		const buttons: ActionButton[] = [];

		buttons.push({
			id: `${item.environment.id}-details`,
			action: 'inspect',
			label: m.common_view_details(),
			onclick: () => void goto(`/environments/${item.environment.id}`),
			icon: InspectIcon
		});

		buttons.push({
			id: `${item.environment.id}-docker-info`,
			action: 'base',
			label: m.common_inspect(),
			disabled: !shouldLoadEnvironment(item.environment),
			onclick: () => openEngineInfo(item),
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

	// Board view mode: cards or table. 'auto' resolves to table once the board
	// grows past a handful of environments (tiles stop scaling — #2778).
	const boardViewPref = new PersistedState<'auto' | 'cards' | 'table'>('dashboard-environments-view', 'auto');
	const boardView = $derived.by((): 'cards' | 'table' => {
		if (boardViewPref.current !== 'auto') return boardViewPref.current;
		return environmentCards.length > 6 ? 'table' : 'cards';
	});

	const environmentTableRows = $derived.by((): EnvironmentTableRow[] => {
		return environmentCards.map(({ environment }) => {
			const loadedItem = overviewById.get(environment.id) ?? createBaseEnvironmentOverview(environment);
			const vInfo = loadedItem.versionInfo;
			return {
				environment,
				online: isEnvironmentOnline(environment),
				loading: isEnvironmentCountsLoading(environment.id),
				running: loadedItem.containers.runningContainers,
				total: loadedItem.containers.totalContainers,
				images: loadedItem.imageUsageCounts.totalImages,
				podCount: loadedItem.podCount ?? null,
				updates: loadedItem.updates,
				vulnerabilities: loadedItem.vulnerabilities,
				versionText: vInfo ? vInfo.displayVersion || vInfo.currentTag || vInfo.currentVersion || 'unknown' : null,
				updateAvailable: !!vInfo?.updateAvailable,
				menuButtons: getEnvironmentActionButtons(loadedItem)
			};
		});
	});

	// Updates hero: aggregate pending image updates across every environment,
	// mirroring the iOS Updates overview header.
	const updatesOverview = $derived({
		pending: (containersQuery.data?.updatesAvailable ?? 0) + (projectsQuery.data?.updatesAvailable ?? 0),
		checking: loadableEnvironmentIds.size > 0 && (containersQuery.isPending || projectsQuery.isPending)
	});

	const summaryTiles = $derived.by(() => {
		const summary = boardSummary;
		let updatesContext: string;
		if (updatesOverview.pending === 0 && !updatesOverview.checking) {
			updatesContext = m.dashboard_updates_up_to_date();
		} else if (updatesOverview.checking) {
			updatesContext = m.dashboard_updates_checking();
		} else {
			updatesContext = m.dashboard_updates_available_label();
		}

		return [
			{
				key: 'updates',
				href: '/updates',
				icon: UpdateIcon,
				label: m.updates(),
				value: updatesOverview.pending,
				context: updatesContext
			},
			{
				key: 'containers',
				href: '/containers',
				icon: ContainersIcon,
				label: m.containers(),
				value: summary.totalContainers,
				context: formatContainerOverviewLabel(summary)
			},
			{
				key: 'images',
				href: '/images',
				icon: ImagesIcon,
				label: m.images(),
				value: summary.totalImages,
				context: formatImageOverviewLabel(summary)
			},
			{
				key: 'volumes',
				href: '/volumes',
				icon: VolumesIcon,
				label: m.resource_volumes_cap(),
				value: summary.totalVolumes,
				context: formatVolumeOverviewLabel(summary)
			},
			...(summary.podmanEnvironments > 0
				? [
						{
							key: 'pods',
							href: '/podman/pods',
							icon: BoxIcon,
							label: m.podman_pods_title(),
							value: summary.totalPods,
							context: m.dashboard_all_podman_environments({ count: summary.podmanEnvironments })
						}
					]
				: [])
		];
	});

	async function openPruneDialog(item: DashboardEnvironmentOverview) {
		if (!canPruneEnvironment(item) || pruneDefaultsLoadingId) {
			return;
		}

		const environmentId = item.environment.id;
		pruneEnvironment = item;
		pruneDefaultsLoadingId = environmentId;
		try {
			const operationResult = await tryCatch((async () => settingsService.getSettingsForEnvironment(environmentId))());
			if (operationResult.error !== null) {
				// Fall back to the dialog's built-in defaults if settings can't be loaded.
				pruneDefaults = null;
			} else {
				pruneDefaults = operationResult.data.values;
			}
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
			networks: m.unused_networks(),
			volumes: m.unused_volumes(),
			buildCache: m.build_cache()
		};
		const typesString = selectedTypes.map((type) => typeLabels[type]).join(', ');

		pruningEnvironmentId = environmentId;

		await handleApiResultWithCallbacks({
			result: await tryCatch(systemService.pruneAllForEnvironment(environmentId, pruneRequest)),
			message: m.dashboard_prune_failed({ types: typesString }),
			setLoadingState: (value) => {
				pruningEnvironmentId = value ? environmentId : null;
			},
			onSuccess: (data) => {
				isPruneDialogOpen = false;
				pruneEnvironment = null;
				pruneDefaults = null;
				const toastOptions = {
					...(activityToastOptions(extractActivityId(data)) ?? {}),
					description: targetEnvironment.environment.name
				};
				if (selectedTypes.length === 1) {
					toast.success(m.dashboard_prune_success_one({ types: typesString }), toastOptions);
				} else {
					toast.success(m.dashboard_prune_success_many({ types: typesString }), toastOptions);
				}
			}
		});
	}
</script>

<div class="flex h-full min-h-0 flex-col gap-4 overflow-hidden pt-3 md:gap-5 md:pt-5">
	<header class="flex shrink-0 items-start justify-between gap-4">
		<div class="min-w-0 space-y-1">
			<p class="text-2xs font-semibold tracking-widest text-muted-foreground uppercase">{m.dashboard_title()}</p>
			<h1 class="text-xl font-semibold tracking-tight sm:text-2xl">{heroGreeting}</h1>
		</div>

		<div class="flex shrink-0 items-center gap-2">
			<div class="inline-flex items-center gap-0.5 rounded-lg border border-border/60 bg-muted/40 p-0.5">
				{#each [{ view: 'cards' as const, icon: LayoutGridIcon, label: m.dashboard_view_cards() }, { view: 'table' as const, icon: LayoutListIcon, label: m.dashboard_view_table() }] as option (option.view)}
					<button
						type="button"
						title={option.label}
						aria-label={option.label}
						aria-pressed={boardView === option.view}
						class={cn(
							'inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
							boardView === option.view && 'bg-primary/15 text-primary ring-1 ring-primary/30 dark:text-primary-tint'
						)}
						onclick={() => (boardViewPref.current = option.view)}
					>
						<option.icon class="size-4" />
					</button>
				{/each}
			</div>

			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					{#snippet child({ props })}
						<ArcaneButton
							{...props}
							action="refresh"
							size="icon"
							class="size-8"
							customLabel={m.common_refresh()}
							loading={isRefreshing}
							onclick={refreshOverview}
						/>
					{/snippet}
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>{m.common_refresh()}</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		</div>
	</header>

	<section class="shrink-0">
		<StatStrip.Root class={summaryTiles.length > 4 ? 'lg:grid-cols-5' : 'lg:grid-cols-4'}>
			{#each summaryTiles as tile (tile.key)}
				<StatStrip.Item label={tile.label} value={tile.value} icon={tile.icon} href={tile.href} loading={boardSummaryLoading}>
					{tile.context}
				</StatStrip.Item>
			{/each}
		</StatStrip.Root>
	</section>

	{#if failedEnvironments.length > 0 || queryErrors.length > 0}
		<section class="shrink-0">
			<Alert.Root variant="warning-subtle" size="sm" icon={AlertTriangleIcon}>
				<div class="flex flex-wrap items-center justify-between gap-2">
					<Alert.Description>
						{#if failedEnvironments.length > 0}
							<p>
								{m.all_environments_unavailable({
									environments: failedEnvironments
										.map(({ environment, message }) => `${environment.name} (${message})`)
										.join(', ')
								})}
							</p>
						{/if}
						{#each withOccurrenceKeys(queryErrors) as error (error.key)}
							<p class="whitespace-pre-line">{error.value}</p>
						{/each}
					</Alert.Description>
					<ArcaneButton action="refresh" size="sm" customLabel={m.common_retry()} onClickPromise={refetchEnvironmentQueries} />
				</div>
			</Alert.Root>
		</section>
	{/if}

	{#if excludedEnvironmentCount > 0}
		<section class="shrink-0">
			<Alert.Root variant="warning-subtle" size="sm" icon={AlertTriangleIcon}>
				<Alert.Description>
					{m.all_environments_excluded({
						count: excludedEnvironmentCount,
						limit: settingsStore.current?.allEnvironmentsLimit ?? 0
					})}
				</Alert.Description>
			</Alert.Root>
		</section>
	{/if}

	<section class="flex min-h-0 flex-1 flex-col overflow-hidden pt-3">
		{#if environmentCards.length === 0}
			<div class="rounded-xl border border-dashed border-border/60 px-4 py-8 text-center">
				<p class="text-sm text-muted-foreground">{m.dashboard_all_no_visible_environments()}</p>
			</div>
		{:else if boardView === 'table'}
			<div class="min-h-0 flex-1 overflow-y-auto pb-2">
				<EnvironmentsTable rows={environmentTableRows} {getLiveStatsState} {getLiveStatsStatus} />
			</div>
		{:else}
			<div class="min-h-0 flex-1 overflow-y-auto pb-2">
				<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
					{#each environmentCards as item (item.environment.id)}
						{@const environment = item.environment}
						{@const overview = overviewById.get(environment.id) ?? createBaseEnvironmentOverview(environment)}
						{@const systemStats = getLiveStatsState(environment.id)?.stats ?? null}
						{@const liveStatsStatus = getLiveStatsStatus(environment)}
						<EnvironmentCard
							{overview}
							{systemStats}
							{liveStatsStatus}
							countsLoading={isEnvironmentCountsLoading(environment.id)}
							menuButtons={getEnvironmentActionButtons(overview)}
							{debugUpgrade}
							debugVersionInfo={DEBUG_VERSION_INFO}
							canUpgrade={canUpgradeEnvironment(environment.id)}
							onRefreshRequested={refreshOverview}
							bind:upgradeOpen={upgradeDialogOpenById[environment.id]}
							bind:upgradeLoading={upgradeDialogUpgradingById[environment.id]}
						/>
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

<DockerInfoDialog
	bind:open={engineInfoOpen}
	engine={engineInfoEngine}
	info={engineInfoData}
	infoPromise={engineInfoPromise}
	errorMessage={engineInfoError}
/>
