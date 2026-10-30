<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createQuery, keepPreviousData } from '@tanstack/svelte-query';
	import { PersistedState } from 'runed';
	import { onDestroy, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { type ActionButton } from '#lib/components/action-button-group/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import PruneConfirmationDialog from '#lib/components/dialogs/prune-confirmation-dialog.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import {
		AddIcon,
		AlertIcon,
		AlertTriangleIcon,
		InspectIcon,
		LayoutGridIcon,
		LayoutListIcon,
		ResetIcon,
		TrashIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { apiKeyService } from '#lib/services/api-key-service.js';
	import type { APIRequestConfig } from '#lib/services/api-service.js';
	import { containerService } from '#lib/services/container-service.js';
	import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
	import { eventService } from '#lib/services/event-service.js';
	import { imageService } from '#lib/services/image-service.js';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import { systemService } from '#lib/services/system-service.js';
	import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { DashboardWidgetLayout } from '#lib/types/auth.js';
	import type { PruneType, SystemPruneRequest } from '#lib/types/automation.js';
	import type { ContainerStatusCounts, ImageUsageCounts } from '#lib/types/docker.js';
	import type { Environment, EnvironmentVulnerabilitySummary } from '#lib/types/environment.js';
	import type { PodmanStatus } from '#lib/types/podman.js';
	import type { AppVersionInformation, Settings } from '#lib/types/settings.js';
	import type { DashboardEnvironmentCardState, DashboardEnvironmentOverview } from '#lib/types/shared.js';
	import type { ProjectStatusCounts } from '#lib/types/swarm.js';
	import { cn } from '#lib/utils.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasAnyPermission, hasPermission } from '#lib/utils/auth.js';
	import { isEnvironmentOnline } from '#lib/utils/docker.js';
	import {
		collectAcrossEnvironments,
		fetchAcrossEnvironments,
		readableEnvironments,
		sumAcrossEnvironments,
		type EnvironmentFailure
	} from '#lib/utils/environments.js';
	import { parseInstant, withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { saveUserPreferences } from '#lib/utils/user-preferences.js';
	import { fetchRiskOverview } from '#lib/utils/vulnerability.js';

	import { canLoadLiveStats, DashboardLiveStats } from '../live-stats.svelte';
	import {
		buildAttentionItems,
		collapseActivities,
		createBaseEnvironmentOverview,
		shouldLoadEnvironment,
		totalReclaimableBytes,
		type AttentionItem,
		type EnvironmentJob
	} from '../overview.helpers';
	import { DASHBOARD_WIDGETS, DEFAULT_DASHBOARD_LAYOUT, resolveDashboardLayout } from '../widgets';
	import ActivityList from './activity-list.svelte';
	import AttentionList from './attention-list.svelte';
	import EnvironmentCard from './environment-card.svelte';
	import EnvironmentUpgradeAction from './environment-upgrade-action.svelte';
	import EnvironmentsTable, { type EnvironmentTableRow } from './environments-table.svelte';
	import ReclaimableSpace from './reclaimable-space.svelte';
	import StatWidget from './stat-widget.svelte';
	import TopMemory from './top-memory.svelte';
	import UpcomingJobs from './upcoming-jobs.svelte';
	import WidgetGrid from './widget-grid.svelte';

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
		isStableVersion: false,
		newestVersion: 'debug-v2',
		updateAvailable: true
	};

	const EMPTY_CONTAINER_COUNTS: ContainerStatusCounts = {
		runningContainers: 0,
		stoppedContainers: 0,
		totalContainers: 0,
		unhealthyContainers: 0,
		updatesAvailable: 0
	};
	const EMPTY_IMAGE_COUNTS: ImageUsageCounts = {
		imagesInuse: 0,
		imagesUnused: 0,
		totalImages: 0,
		totalImageSize: 0
	};
	const CONTAINER_EVENT_TYPES = ['container.oom', 'container.die'];
	const ALL_API_KEYS = { pagination: { page: 1, limit: -1 } };
	const API_KEY_EXPIRY_WINDOW_HOURS = 14 * 24;

	let isRefreshing = $state(false);
	let isPruneDialogOpen = $state(false);
	let pruneEnvironment = $state<DashboardEnvironmentOverview | null>(null);
	let pruneDefaults = $state<Settings | null>(null);
	let pruneDefaultsLoadingId = $state<string | null>(null);
	let pruningEnvironmentId = $state<string | null>(null);
	const liveStats = new DashboardLiveStats();
	const upgradeDialogOpenById = $state<Record<string, boolean>>({});
	const upgradeDialogUpgradingById = $state<Record<string, boolean>>({});

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

	const environmentCards = $derived.by((): DashboardEnvironmentCardState[] => {
		return [...availableEnvironments]
			.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.id.localeCompare(b.id))
			.map((environment) => ({ environment }));
	});
	const loadableEnvironmentCards = $derived(environmentCards.filter(({ environment }) => shouldLoadEnvironment(environment)));
	const loadableEnvironmentIds = $derived(new Set(loadableEnvironmentCards.map(({ environment }) => environment.id)));

	function loadableEnvironments(permission: string): EnvironmentRef[] {
		return readableEnvironments(permission).filter((environment) => loadableEnvironmentIds.has(environment.id));
	}

	function dashboardQueryKey(domain: string, name: string, environments: EnvironmentRef[]) {
		return [domain, environments.map((environment) => environment.id).join(','), 'dashboard', name];
	}

	function byEnvironmentId<C>(result: { byEnvironment: { environment: EnvironmentRef; data: C }[] } | undefined): Map<string, C> {
		return new Map((result?.byEnvironment ?? []).map(({ environment, data }) => [environment.id, data]));
	}

	function isVersionInformation(value: Partial<AppVersionInformation> | undefined): value is AppVersionInformation {
		return value?.currentVersion !== undefined;
	}

	async function collectByEnvironment<C>(
		environments: EnvironmentRef[],
		run: (environmentId: string, config: APIRequestConfig) => Promise<C>
	) {
		const { values, failures } = await collectAcrossEnvironments(environments, run);
		return {
			byEnvironment: values.map(({ environment, value }) => ({ environment, data: value })),
			failedEnvironments: failures,
			excludedEnvironments: [] as EnvironmentRef[]
		};
	}

	const containersQuery = createQuery(() => {
		const environments = loadableEnvironments('containers:list');
		return {
			queryKey: dashboardQueryKey('containers', 'status-counts', environments),
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
			queryKey: dashboardQueryKey('projects', 'status-counts', environments),
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
			queryKey: dashboardQueryKey('images', 'usage-counts', environments),
			queryFn: () =>
				sumAcrossEnvironments(
					environments,
					(scope, config) => imageService.getImageUsageCountsForEnvironment(scope, config),
					EMPTY_IMAGE_COUNTS
				),
			placeholderData: keepPreviousData
		};
	});

	const vulnerabilitiesQuery = createQuery(() => {
		const environments = loadableEnvironments('vulnerabilities:read').filter((environment) =>
			featureStore.isEnabled('vulnerabilityManagement', environment.id)
		);
		return {
			queryKey: dashboardQueryKey('vulnerabilities', 'summary', environments),
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
			queryKey: dashboardQueryKey('podman', 'status', environments),
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
			queryKey: dashboardQueryKey('environments', 'versions', environments),
			queryFn: () =>
				sumAcrossEnvironments<Partial<AppVersionInformation>>(
					environments,
					(scope, config) => environmentManagementService.getVersion(scope, config),
					{}
				),
			placeholderData: keepPreviousData
		};
	});

	const riskEnvironments = $derived(
		loadableEnvironments('vulnerabilities:read').filter((environment) =>
			featureStore.isEnabled('vulnerabilityManagement', environment.id)
		)
	);

	const riskQuery = createQuery(() => {
		const environments = riskEnvironments;
		return {
			queryKey: dashboardQueryKey('vulnerabilities', 'risk', environments),
			queryFn: () => fetchRiskOverview(environments),
			enabled: environments.length > 0,
			placeholderData: keepPreviousData
		};
	});

	const eventsQuery = createQuery(() => {
		const environments = loadableEnvironments('events:read');
		return {
			queryKey: dashboardQueryKey('events', 'container-events', environments),
			queryFn: () =>
				collectByEnvironment(environments, (environmentId, config) =>
					eventService.getEnvironmentEvents(
						environmentId,
						{
							pagination: { page: 1, limit: 20 },
							sort: { column: 'timestamp', direction: 'desc' },
							filters: { type: CONTAINER_EVENT_TYPES }
						},
						config
					)
				),
			placeholderData: keepPreviousData
		};
	});

	const jobsQuery = createQuery(() => {
		const environments = loadableEnvironments('jobs:manage');
		return {
			queryKey: dashboardQueryKey('environment-jobs', 'jobs', environments),
			queryFn: () =>
				collectByEnvironment(environments, (environmentId, config) => jobScheduleService.listJobs(environmentId, config)),
			placeholderData: keepPreviousData
		};
	});

	const diskUsageQuery = createQuery(() => {
		const environments = loadableEnvironments('system:read');
		return {
			queryKey: dashboardQueryKey('system', 'disk-usage', environments),
			queryFn: () => collectByEnvironment(environments, (environmentId) => systemService.getDiskUsage(environmentId)),
			placeholderData: keepPreviousData
		};
	});

	const topMemoryQuery = createQuery(() => {
		const environments = loadableEnvironments('containers:read');
		return {
			queryKey: dashboardQueryKey('containers', 'top-memory', environments),
			queryFn: () =>
				fetchAcrossEnvironments(
					environments,
					{ pagination: { page: 1, limit: 5 }, sort: { column: 'memoryUsage', direction: 'desc' } },
					(scope, options, config) => containerService.getContainersForEnvironment(scope, options, config)
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
		vulnerabilitiesQuery,
		podmanQuery,
		versionsQuery,
		riskQuery,
		eventsQuery,
		jobsQuery,
		diskUsageQuery,
		topMemoryQuery
	];

	const containersByEnvironment = $derived(byEnvironmentId(containersQuery.data));
	const projectsByEnvironment = $derived(byEnvironmentId(projectsQuery.data));
	const imagesByEnvironment = $derived(byEnvironmentId(imagesQuery.data));
	const vulnerabilitiesByEnvironment = $derived(byEnvironmentId(vulnerabilitiesQuery.data));
	const podmanByEnvironment = $derived(byEnvironmentId(podmanQuery.data));
	const versionsByEnvironment = $derived(byEnvironmentId(versionsQuery.data));
	const riskByEnvironment = $derived(byEnvironmentId(riskQuery.data));
	const diskUsageByEnvironment = $derived(byEnvironmentId(diskUsageQuery.data));

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
		Math.max(0, ...environmentQueries.map((query) => query.data?.excludedEnvironments?.length ?? 0))
	);

	const expiringApiKeys = $derived.by(() => {
		const cutoff = Temporal.Now.instant().add({ hours: API_KEY_EXPIRY_WINDOW_HOURS });
		return (apiKeysQuery.data?.data ?? []).filter((apiKey) => {
			const expiresAt = parseInstant(apiKey.expiresAt);
			return expiresAt !== null && Temporal.Instant.compare(expiresAt, cutoff) <= 0;
		});
	});

	function buildEnvironmentOverview(environment: Environment): DashboardEnvironmentOverview {
		const containers = containersByEnvironment.get(environment.id) ?? EMPTY_CONTAINER_COUNTS;
		const updates = containers.updatesAvailable + (projectsByEnvironment.get(environment.id)?.updatesAvailable ?? 0);
		const vulnerabilities = vulnerabilitiesByEnvironment.get(environment.id)?.actionableCount ?? 0;
		const expiringKeys = environment.id === LOCAL_DOCKER_ENVIRONMENT_ID ? expiringApiKeys.length : 0;
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

	function isEnvironmentCountsLoading(environmentId: string): boolean {
		return loadableEnvironmentIds.has(environmentId) && !containersByEnvironment.has(environmentId) && containersQuery.isFetching;
	}

	const boardSummaryLoading = $derived(loadableEnvironmentIds.size > 0 && containersQuery.isPending);

	$effect(() => {
		const environmentsToLoad = loadableEnvironmentCards.map(({ environment }) => environment);
		const liveEnvironments = environmentsToLoad.filter(canLoadLiveStats);

		untrack(() => {
			liveStats.sync(liveEnvironments);
			for (const environment of environmentsToLoad) {
				void featureStore.load(environment.id);
			}
		});
	});

	onDestroy(() => {
		liveStats.destroy();
	});

	async function refetchEnvironmentQueries() {
		await Promise.all([...environmentQueries, apiKeysQuery].map((query) => query.refetch()));
	}

	async function refreshOverview() {
		isRefreshing = true;
		try {
			liveStats.retryFailed();
			await refetchEnvironmentQueries();
		} finally {
			isRefreshing = false;
		}
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
			placement: 'primary',
			iconOnly: true,
			label: m.common_view_details(),
			onclick: () => void goto(resolve('/(app)/environments/[id]', { id: item.environment.id })),
			icon: InspectIcon
		});

		if (canPruneInEnvironment(item.environment.id)) {
			buttons.push({
				id: `${item.environment.id}-prune`,
				action: 'prune',
				placement: 'primary',
				iconOnly: true,
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
	const boardViewPref = new PersistedState<'auto' | 'cards' | 'table'>('dashboard-environments-view', 'table');
	const boardView = $derived.by((): 'cards' | 'table' => {
		if (boardViewPref.current !== 'auto') return boardViewPref.current;
		return environmentCards.length > 6 ? 'table' : 'cards';
	});

	const environmentJobs = $derived(
		(jobsQuery.data?.byEnvironment ?? []).flatMap(({ environment, data }): EnvironmentJob[] =>
			data.jobs.flatMap((job) => [job, ...(job.children ?? [])]).map((job) => ({ environmentId: environment.id, job }))
		)
	);

	const shownEnvironmentIds = $derived(new Set(environmentCards.map(({ environment }) => environment.id)));
	const activityGroups = $derived(
		collapseActivities(
			activityStore.activities.filter((activity) =>
				shownEnvironmentIds.has(activity.sourceEnvironmentId || activity.environmentId)
			)
		)
	);

	let editingLayout = $state(false);
	let draftLayout = $state.raw<DashboardWidgetLayout[]>([]);
	const savedLayout = $derived(resolveDashboardLayout(userStore.current?.preferences?.dashboardLayout));
	const layout = $derived(editingLayout ? draftLayout : savedLayout);
	const hiddenWidgets = $derived(layout.filter((widget) => widget.hidden));
	const layoutHasChanges = $derived(editingLayout && JSON.stringify(draftLayout) !== JSON.stringify(savedLayout));

	const attentionItems = $derived(
		buildAttentionItems({
			environments: environmentCards.map(({ environment }) => environment),
			containers: containersByEnvironment,
			events: (eventsQuery.data?.byEnvironment ?? []).flatMap(({ environment, data }) =>
				data.data.map((event) => ({ environmentId: environment.id, event }))
			),
			findings: riskQuery.data?.riskiestFindings ?? [],
			stats: liveStats.statsByEnvironment,
			jobs: environmentJobs,
			activityGroups,
			expiringApiKeys,
			versions: new Map(
				[...overviewById.values()].flatMap((item) => (item.versionInfo ? [[item.environment.id, item.versionInfo] as const] : []))
			),
			upgradable: new Set(
				environmentCards.map(({ environment }) => environment.id).filter((environmentId) => canUpgradeEnvironment(environmentId))
			),
			now: Temporal.Now.instant()
		})
	);
	const attentionFailed = $derived(
		[containersQuery, eventsQuery, riskQuery, jobsQuery, versionsQuery].some((query) => query.isError)
	);
	const attentionCountByEnvironment = $derived(
		attentionItems.reduce(
			(counts, item) => counts.set(item.environmentId, (counts.get(item.environmentId) ?? 0) + 1),
			new Map<string, number>()
		)
	);

	const environmentTableRows = $derived.by((): EnvironmentTableRow[] => {
		return environmentCards
			.map(({ environment }) => {
				const loadedItem = overviewById.get(environment.id) ?? createBaseEnvironmentOverview(environment);
				const vInfo = loadedItem.versionInfo;
				const risk = riskByEnvironment.get(environment.id);
				const diskUsageEntry = diskUsageByEnvironment.get(environment.id);
				return {
					environment,
					online: isEnvironmentOnline(environment),
					loading: isEnvironmentCountsLoading(environment.id),
					running: loadedItem.containers.runningContainers,
					total: loadedItem.containers.totalContainers,
					images: loadedItem.imageUsageCounts.totalImages,
					podCount: loadedItem.podCount ?? null,
					updates: loadedItem.updates,
					reclaimable: diskUsageEntry ? totalReclaimableBytes(diskUsageEntry) : null,
					risk: risk ? { score: risk.riskScore, band: risk.riskBand, status: risk.scoreStatus } : null,
					versionText: vInfo ? vInfo.displayVersion || vInfo.currentTag || vInfo.currentVersion || 'unknown' : null,
					updateAvailable: !!vInfo?.updateAvailable,
					menuButtons: getEnvironmentActionButtons(loadedItem)
				};
			})
			.sort(
				(a, b) =>
					Number(a.online) - Number(b.online) ||
					(attentionCountByEnvironment.get(b.environment.id) ?? 0) - (attentionCountByEnvironment.get(a.environment.id) ?? 0) ||
					a.environment.name.localeCompare(b.environment.name, undefined, { sensitivity: 'base' })
			);
	});

	// Updates hero: aggregate pending image updates across every environment,
	// mirroring the iOS Updates overview header.
	const updatesOverview = $derived({
		pending: (containersQuery.data?.updatesAvailable ?? 0) + (projectsQuery.data?.updatesAvailable ?? 0),
		checking: loadableEnvironmentIds.size > 0 && (containersQuery.isPending || projectsQuery.isPending)
	});

	const offlineEnvironments = $derived(
		environmentCards.filter(({ environment }) => !isEnvironmentOnline(environment)).map(({ environment }) => environment)
	);
	const diskUsage = $derived(diskUsageQuery.data?.byEnvironment ?? []);

	function handleAttentionAction(item: AttentionItem) {
		if (item.target.kind === 'upgrade') {
			upgradeDialogOpenById[item.environmentId] = true;
		}
	}

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
					...activityToastOptions(extractActivityId(data)),
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

<div class="flex flex-col gap-8 pt-3 pb-6 md:pt-5">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<h1 class="min-w-0 text-xl font-semibold tracking-tight sm:text-2xl">{heroGreeting}</h1>

		<div class="flex shrink-0 flex-wrap items-center justify-end gap-2">
			{#if editingLayout}
				<DropdownMenu.Root>
					<DropdownMenu.Trigger disabled={hiddenWidgets.length === 0}>
						{#snippet child({ props })}
							<ArcaneButton {...props} action="base" size="sm" icon={AddIcon} customLabel={m.add_widget()} />
						{/snippet}
					</DropdownMenu.Trigger>
					<DropdownMenu.Content align="end">
						{#each hiddenWidgets as widget (widget.id)}
							<DropdownMenu.Item
								onclick={() =>
									(draftLayout = draftLayout.map((entry) => (entry.id === widget.id ? { ...entry, hidden: false } : entry)))}
							>
								{DASHBOARD_WIDGETS[widget.id].label()}
							</DropdownMenu.Item>
						{/each}
					</DropdownMenu.Content>
				</DropdownMenu.Root>
				<ArcaneButton
					action="base"
					size="sm"
					icon={ResetIcon}
					customLabel={m.reset_to_default()}
					onclick={() => (draftLayout = DEFAULT_DASHBOARD_LAYOUT)}
				/>
				{#if layoutHasChanges}
					<span class="mx-1 text-xs text-warning">{m.common_unsaved_changes()}</span>
					<ArcaneButton
						action="base"
						tone="outline"
						size="sm"
						class="gap-2"
						icon={ResetIcon}
						customLabel={m.common_reset()}
						onclick={() => (draftLayout = savedLayout)}
					/>
				{/if}
				<ArcaneButton action="cancel" size="sm" onclick={() => (editingLayout = false)} />
				<ArcaneButton
					action="save"
					size="sm"
					class="min-w-20 gap-2"
					disabled={!layoutHasChanges}
					onClickPromise={async () => {
						await saveUserPreferences({ dashboardLayout: draftLayout });
						editingLayout = false;
					}}
				/>
			{:else}
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
				<ArcaneTooltip.Root>
					<ArcaneTooltip.Trigger>
						{#snippet child({ props })}
							<ArcaneButton
								{...props}
								action="edit"
								size="icon"
								class="size-8"
								customLabel={m.edit_layout()}
								onclick={() => {
									draftLayout = savedLayout;
									editingLayout = true;
								}}
							/>
						{/snippet}
					</ArcaneTooltip.Trigger>
					<ArcaneTooltip.Content>{m.edit_layout()}</ArcaneTooltip.Content>
				</ArcaneTooltip.Root>
			{/if}
		</div>
	</header>

	{#if failedEnvironments.length > 0 || queryErrors.length > 0}
		<section>
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
		<section>
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

	<WidgetGrid {layout} editing={editingLayout} onChange={(next) => (draftLayout = next)}>
		{#snippet widget(id)}
			{#if id === 'healthy' || id === 'attentionCount' || id === 'updates' || id === 'risk' || id === 'reclaimableTotal'}
				<StatWidget
					{id}
					healthy={{
						online: environmentCards.length - offlineEnvironments.length,
						total: environmentCards.length,
						offlineNames: offlineEnvironments.map((environment) => environment.name)
					}}
					attention={{
						count: attentionItems.length,
						critical: attentionItems.filter((item) => item.severity === 'critical').length,
						loading: boardSummaryLoading
					}}
					updates={updatesOverview}
					risk={{
						score: riskQuery.data && riskQuery.data.scoreStatus !== 'unavailable' ? riskQuery.data.riskScore : null,
						delta: riskQuery.data?.delta7d,
						knownExploited: riskQuery.data?.drivers.knownExploited ?? 0,
						loading: riskEnvironments.length > 0 && riskQuery.isPending
					}}
					reclaimable={{
						bytes: diskUsageQuery.isError ? null : diskUsage.reduce((sum, entry) => sum + totalReclaimableBytes(entry.data), 0),
						environments: diskUsage.length,
						loading: loadableEnvironmentIds.size > 0 && diskUsageQuery.isPending
					}}
				/>
			{:else if id === 'environments'}
				<section class="min-w-0">
					{#if environmentCards.length === 0}
						<div class="rounded-xl border border-dashed border-border/60 px-4 py-8 text-center">
							<p class="text-sm text-muted-foreground">{m.dashboard_all_no_visible_environments()}</p>
						</div>
					{:else if boardView === 'table'}
						<EnvironmentsTable
							rows={environmentTableRows}
							getLiveStatsState={(environmentId) => liveStats.get(environmentId)}
							getLiveStatsStatus={(environment) => liveStats.status(environment)}
						/>
					{:else}
						<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
							{#each environmentCards as item (item.environment.id)}
								{@const environment = item.environment}
								{@const overview = overviewById.get(environment.id) ?? createBaseEnvironmentOverview(environment)}
								{@const systemStats = liveStats.get(environment.id)?.stats ?? null}
								{@const liveStatsStatus = liveStats.status(environment)}
								<svelte:boundary>
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

									{#snippet failed(_renderError: unknown, reset: () => void)}
										<Alert.Root variant="destructive" icon={AlertIcon} heading={m.error_generic()}>
											<div class="mt-3">
												<ArcaneButton action="refresh" size="sm" customLabel={m.common_retry()} onclick={reset} />
											</div>
										</Alert.Root>
									{/snippet}
								</svelte:boundary>
							{/each}
						</div>
					{/if}
				</section>
			{:else if id === 'attention'}
				<AttentionList
					items={attentionItems}
					loading={boardSummaryLoading}
					failed={attentionFailed}
					onAction={handleAttentionAction}
				/>
			{:else if id === 'activity'}
				<ActivityList groups={activityGroups} loading={activityStore.loading} unavailable={activityStore.streamError} />
			{:else if id === 'jobs'}
				<UpcomingJobs
					jobs={environmentJobs}
					loading={jobsQuery.isPending && loadableEnvironmentIds.size > 0}
					failed={jobsQuery.isError}
				/>
			{:else if id === 'reclaimable'}
				<ReclaimableSpace
					usage={diskUsage}
					loading={diskUsageQuery.isPending && loadableEnvironmentIds.size > 0}
					failed={diskUsageQuery.isError}
				/>
			{:else}
				<TopMemory
					containers={topMemoryQuery.data?.data ?? []}
					loading={topMemoryQuery.isPending && loadableEnvironmentIds.size > 0}
					failed={topMemoryQuery.isError}
				/>
			{/if}
		{/snippet}
	</WidgetGrid>
</div>

{#each environmentCards as { environment } (environment.id)}
	{@const versionInfo = overviewById.get(environment.id)?.versionInfo ?? (debugUpgrade ? DEBUG_VERSION_INFO : null)}
	{#if versionInfo && (versionInfo.updateAvailable || debugUpgrade)}
		<EnvironmentUpgradeAction
			{environment}
			{versionInfo}
			canUpgrade={canUpgradeEnvironment(environment.id)}
			debug={debugUpgrade}
			onRefreshRequested={refreshOverview}
			render="dialog"
			bind:open={upgradeDialogOpenById[environment.id]}
			bind:upgrading={upgradeDialogUpgradingById[environment.id]}
		/>
	{/if}
{/each}

<PruneConfirmationDialog
	open={isPruneDialogOpen}
	isPruning={!!pruningEnvironmentId}
	defaults={pruneDefaults}
	onConfirm={confirmPrune}
	onCancel={closePruneDialog}
/>
