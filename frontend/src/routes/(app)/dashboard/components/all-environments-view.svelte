<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createQuery, keepPreviousData } from '@tanstack/svelte-query';
	import { PersistedState } from 'runed';
	import { onDestroy, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { MediaQuery } from 'svelte/reactivity';

	import { type ActionButton } from '#lib/components/action-button-group/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import PruneConfirmationDialog from '#lib/components/dialogs/prune-confirmation-dialog.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { AddIcon, AlertIcon, InspectIcon, LayoutGridIcon, LayoutListIcon, ResetIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { apiKeyListQuery } from '#lib/query/api-keys.js';
	import { containerListQuery } from '#lib/query/containers.js';
	import {
		dashboardContainerCountsQuery,
		dashboardDiskUsageQuery,
		dashboardImageCountsQuery,
		dashboardJobsQuery,
		dashboardPodmanStatusQuery,
		dashboardProjectCountsQuery,
		dashboardRecentEventsQuery,
		dashboardRiskQuery,
		dashboardVersionsQuery,
		dashboardVulnerabilitySummaryQuery,
		EMPTY_CONTAINER_COUNTS,
		EMPTY_IMAGE_COUNTS
	} from '#lib/query/dashboard.js';
	import { emptySettings, settingsService } from '#lib/services/settings-service.js';
	import { systemService } from '#lib/services/system-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore, isLocalDockerDisabled, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { Environment } from '#lib/types/environment.js';
	import type { Settings } from '#lib/types/settings.js';
	import type { PruneAllRequest } from '#lib/types/system.js';
	import type { DashboardEnvironmentCardState, DashboardEnvironmentOverview } from '#lib/types/ui/dashboard.js';
	import type { PruneType } from '#lib/types/ui/system.js';
	import type { BuiltinDashboardWidgetType } from '#lib/types/ui/user.js';
	import type { VersionInfo } from '#lib/types/version.js';
	import { cn } from '#lib/utils.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasAnyPermission, hasPermission } from '#lib/utils/auth.js';
	import {
		addDashboardWidget,
		DASHBOARD_ENVIRONMENTS_VIEW_KEY,
		DASHBOARD_WIDGET_TYPES,
		type DashboardWidget,
		defaultDashboardLayout,
		type EnvironmentsBoardViewPref,
		type EnvironmentsLayoutOptions,
		MAX_DASHBOARD_WIDGETS,
		resolveDashboardLayout,
		resolveEnvironmentsBoardView,
		showWidget
	} from '#lib/utils/dashboard-layout.js';
	import { isEnvironmentOnline } from '#lib/utils/docker.js';
	import { readableEnvironments, readEachEnvironment, type EnvironmentFailure } from '#lib/utils/environments.js';
	import { withOccurrenceKeys } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { saveUserPreferences } from '#lib/utils/user-preferences.js';

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
	import ActivityList from './activity-list.svelte';
	import AttentionList from './attention-list.svelte';
	import DashboardPanel from './dashboard-panel.svelte';
	import EnvironmentCard from './environment-card.svelte';
	import EnvironmentUpgradeAction from './environment-upgrade-action.svelte';
	import EnvironmentsTable, { type EnvironmentTableRow } from './environments-table.svelte';
	import ReclaimableSpace from './reclaimable-space.svelte';
	import StatWidget from './stat-widget.svelte';
	import TopMemory from './top-memory.svelte';
	import UpcomingJobs from './upcoming-jobs.svelte';
	import WidgetGrid from './widget-grid.svelte';
	import CustomWidget from './widgets/custom-widget.svelte';
	import WidgetSheet from './widgets/widget-sheet.svelte';

	let {
		heroGreeting,
		debugUpgrade = false
	}: {
		heroGreeting: string;
		debugUpgrade?: boolean;
	} = $props();

	const DEBUG_VERSION_INFO: VersionInfo = {
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

	const EXPIRING_API_KEYS = { pagination: { page: 1, limit: -1 }, filters: { expiresWithinDays: 14 } };
	const TOP_MEMORY_REQUEST = {
		pagination: { page: 1, limit: 5 },
		sort: { column: 'memoryUsage', direction: 'desc' as const }
	};
	const ATTENTION_WIDGETS: BuiltinDashboardWidgetType[] = ['attention', 'attentionCount', 'environments'];
	const PRUNE_SETTING_GROUPS: (keyof Settings)[][] = [
		['pruneContainerMode', 'pruneContainerUntil'],
		['pruneImageMode', 'pruneImageUntil'],
		['pruneNetworkMode', 'pruneNetworkUntil'],
		['pruneVolumeMode'],
		['pruneBuildCacheMode', 'pruneBuildCacheUntil']
	];

	let isRefreshing = $state(false);
	let isPruneDialogOpen = $state(false);
	let pruneTargetId = $state<string | null>(null);
	let pruneDefaults = $state<Partial<Settings> | null>(null);
	let pruneDefaultsLoadingId = $state<string | null>(null);
	let pruningEnvironmentId = $state<string | null>(null);
	const liveStats = new DashboardLiveStats();
	const upgradeDialogOpenById = $state<Record<string, boolean>>({});
	const upgradeDialogUpgradingById = $state<Record<string, boolean>>({});

	const availableEnvironments = $derived.by(() => {
		if (!userStore.current) {
			return [];
		}

		return environmentStore.available.filter((environment) => environment.enabled && !isLocalDockerDisabled(environment));
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

	const isMobile = new IsMobile();
	const hasTwoCardColumns = new MediaQuery('(width >= 80rem)');
	const boardViewPref = new PersistedState<EnvironmentsBoardViewPref>(DASHBOARD_ENVIRONMENTS_VIEW_KEY, 'table');
	const boardView = $derived(resolveEnvironmentsBoardView(boardViewPref.current, environmentCards.length, isMobile.current));
	const environmentsLayout = $derived<EnvironmentsLayoutOptions>({
		view: boardView,
		cardColumns: hasTwoCardColumns.current ? 2 : 1
	});

	let editingLayout = $state(false);
	let draftLayout = $state.raw<DashboardWidget[]>([]);
	const savedLayout = $derived(
		resolveDashboardLayout(userStore.current?.preferences?.dashboardLayout, environmentCards.length, environmentsLayout)
	);
	const layout = $derived(editingLayout ? draftLayout : savedLayout);
	const hiddenWidgets = $derived(layout.filter((widget) => widget.hidden && !DASHBOARD_WIDGET_TYPES[widget.type].multi));
	const widgetLimitReached = $derived(draftLayout.length >= MAX_DASHBOARD_WIDGETS);
	let widgetSheet = $state<WidgetSheet>();
	const layoutHasChanges = $derived(editingLayout && JSON.stringify(draftLayout) !== JSON.stringify(savedLayout));
	const visibleWidgets = $derived(new Set(layout.filter((widget) => !widget.hidden).map((widget) => widget.type)));

	function anyWidgetVisible(types: BuiltinDashboardWidgetType[]): boolean {
		return types.some((type) => visibleWidgets.has(type));
	}

	function loadableEnvironments(permission: string): EnvironmentRef[] {
		return readableEnvironments(permission).filter((environment) => loadableEnvironmentIds.has(environment.id));
	}

	function byEnvironmentId<C>(result: { byEnvironment: { environment: EnvironmentRef; data: C }[] } | undefined): Map<string, C> {
		return new Map((result?.byEnvironment ?? []).map(({ environment, data }) => [environment.id, data]));
	}

	function isVersionInformation(value: Partial<VersionInfo> | undefined): value is VersionInfo {
		return value?.currentVersion !== undefined;
	}

	const containersQuery = createQuery(() => ({
		...dashboardContainerCountsQuery(loadableEnvironments('containers:list')),
		placeholderData: keepPreviousData
	}));

	const projectsQuery = createQuery(() => ({
		...dashboardProjectCountsQuery(loadableEnvironments('projects:list')),
		placeholderData: keepPreviousData
	}));

	const imagesQuery = createQuery(() => ({
		...dashboardImageCountsQuery(loadableEnvironments('images:list')),
		placeholderData: keepPreviousData
	}));

	const vulnerabilitiesQuery = createQuery(() => ({
		...dashboardVulnerabilitySummaryQuery(
			loadableEnvironments('vulnerabilities:list').filter((environment) =>
				featureStore.isEnabled('vulnerabilityManagement', environment.id)
			)
		),
		placeholderData: keepPreviousData
	}));

	const podmanQuery = createQuery(() => ({
		...dashboardPodmanStatusQuery(loadableEnvironments('podman:read')),
		placeholderData: keepPreviousData
	}));

	const versionsQuery = createQuery(() => ({
		...dashboardVersionsQuery(loadableEnvironments('environments:read')),
		placeholderData: keepPreviousData
	}));

	const riskEnvironments = $derived(
		loadableEnvironments('vulnerabilities:list').filter((environment) =>
			featureStore.isEnabled('vulnerabilityManagement', environment.id)
		)
	);

	const riskQuery = createQuery(() => ({
		...dashboardRiskQuery(riskEnvironments),
		enabled: riskEnvironments.length > 0 && anyWidgetVisible(['risk', ...ATTENTION_WIDGETS]),
		placeholderData: keepPreviousData
	}));

	const eventsQuery = createQuery(() => ({
		...dashboardRecentEventsQuery(userStore.hasPermission('events:list') ? loadableEnvironments('events:list') : []),
		enabled: anyWidgetVisible(ATTENTION_WIDGETS),
		placeholderData: keepPreviousData
	}));

	const jobsQuery = createQuery(() => ({
		...dashboardJobsQuery(loadableEnvironments('jobs:list')),
		enabled: anyWidgetVisible(['jobs', ...ATTENTION_WIDGETS]),
		placeholderData: keepPreviousData
	}));

	const diskUsageQuery = createQuery(() => ({
		...dashboardDiskUsageQuery(loadableEnvironments('system:read')),
		enabled: anyWidgetVisible(['reclaimable', 'reclaimableTotal', 'environments']),
		placeholderData: keepPreviousData
	}));

	const topMemoryQuery = createQuery(() => {
		const environments = loadableEnvironments('containers:read');
		return {
			...containerListQuery(environments.map((environment) => environment.id).join(','), environments, TOP_MEMORY_REQUEST),
			enabled: anyWidgetVisible(['memory']),
			placeholderData: keepPreviousData
		};
	});

	const apiKeysQuery = createQuery(() => ({
		...apiKeyListQuery(EXPIRING_API_KEYS),
		enabled: userStore.hasPermission('apikeys:list')
	}));

	const summaryQueries = [
		containersQuery,
		projectsQuery,
		imagesQuery,
		vulnerabilitiesQuery,
		podmanQuery,
		versionsQuery,
		riskQuery,
		eventsQuery,
		jobsQuery,
		topMemoryQuery
	];
	const environmentQueries = [...summaryQueries, diskUsageQuery];

	const containersByEnvironment = $derived(byEnvironmentId(containersQuery.data));
	const projectsByEnvironment = $derived(byEnvironmentId(projectsQuery.data));
	const imagesByEnvironment = $derived(byEnvironmentId(imagesQuery.data));
	const vulnerabilitiesByEnvironment = $derived(byEnvironmentId(vulnerabilitiesQuery.data));
	const podmanByEnvironment = $derived(byEnvironmentId(podmanQuery.data));
	const versionsByEnvironment = $derived(byEnvironmentId(versionsQuery.data));
	const riskByEnvironment = $derived(byEnvironmentId(riskQuery.data));
	const diskUsageByEnvironment = $derived(byEnvironmentId(diskUsageQuery.data));

	const failedEnvironments = $derived.by((): EnvironmentFailure[] => {
		const failures = summaryQueries.flatMap((query) => query.data?.failedEnvironments ?? []);
		return failures.filter(
			(failure, index) => failures.findIndex((other) => other.environment.id === failure.environment.id) === index
		);
	});
	const failedEnvironmentMessages = $derived(
		new Map(
			Array.from(
				Map.groupBy(
					summaryQueries.flatMap((query) => query.data?.failedEnvironments ?? []),
					(failure) => failure.environment.id
				),
				([environmentId, failures]): [string, string[]] => [environmentId, failures.map((failure) => failure.message)]
			)
		)
	);
	const queryErrors = $derived(
		[...summaryQueries, apiKeysQuery].flatMap((query) => (query.error ? [extractApiErrorMessage(query.error)] : []))
	);
	const excludedEnvironmentCount = $derived(
		Math.max(0, ...environmentQueries.map((query) => query.data?.excludedEnvironments?.length ?? 0))
	);

	const expiringApiKeys = $derived(apiKeysQuery.data?.data ?? []);

	function buildEnvironmentOverview(environment: Environment): DashboardEnvironmentOverview {
		const containers = containersByEnvironment.get(environment.id) ?? EMPTY_CONTAINER_COUNTS;
		const updates = containers.updatesAvailable + (projectsByEnvironment.get(environment.id)?.updatesAvailable ?? 0);
		const vulnerabilities = vulnerabilitiesByEnvironment.get(environment.id)?.actionableCount ?? 0;
		const podman = podmanByEnvironment.get(environment.id);
		const versionInfo = versionsByEnvironment.get(environment.id);

		return {
			environment,
			containers,
			imageUsageCounts: imagesByEnvironment.get(environment.id) ?? EMPTY_IMAGE_COUNTS,
			updates,
			vulnerabilities,
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
			void featureStore.load(environmentsToLoad.map((environment) => environment.id));
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
			onclick: () => void goto(resolve('/(app)/agents/[id]', { id: item.environment.id })),
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
				onclick: () => void openPruneDialog(item.environment.id)
			});
		}

		return buttons;
	}

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

	const attentionItems = $derived(
		buildAttentionItems({
			environments: environmentCards.map(({ environment }) => environment),
			containers: containersByEnvironment,
			images: imagesByEnvironment,
			events: (eventsQuery.data?.events ?? []).flatMap((event) =>
				event.environmentId ? [{ environmentId: event.environmentId, event }] : []
			),
			findings: riskQuery.data?.riskiestFindings ?? [],
			disk: liveStats.diskPressureByEnvironment,
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
	const recentActivityGroups = $derived.by(() => {
		if (!visibleWidgets.has('attention')) return activityGroups;
		const shown = new Set(attentionItems.map((item) => item.activityKey));
		return activityGroups.filter((group) => !shown.has(group.key));
	});
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

	const updatesOverview = $derived({
		pending: (containersQuery.data?.updatesAvailable ?? 0) + (projectsQuery.data?.updatesAvailable ?? 0),
		upToDate: Math.max(0, (containersQuery.data?.totalContainers ?? 0) - (containersQuery.data?.updatesAvailable ?? 0)),
		checking: loadableEnvironmentIds.size > 0 && (containersQuery.isPending || projectsQuery.isPending)
	});

	const offlineEnvironments = $derived(
		environmentCards.filter(({ environment }) => !isEnvironmentOnline(environment)).map(({ environment }) => environment)
	);
	const diskUsage = $derived(diskUsageQuery.data?.byEnvironment ?? []);
	const diskUsageFailures = $derived(diskUsageQuery.data?.failedEnvironments ?? []);

	function handleAttentionAction(item: AttentionItem) {
		if (item.target.kind === 'upgrade') {
			upgradeDialogOpenById[item.environmentId] = true;
		}
	}

	const prunableEnvironments = $derived(
		environmentCards
			.map(({ environment }) => overviewById.get(environment.id) ?? createBaseEnvironmentOverview(environment))
			.filter(canPruneEnvironment)
	);

	async function openPruneDialog(requestedId: string) {
		if (pruneDefaultsLoadingId) {
			return;
		}

		const [onlyEnvironment] = prunableEnvironments;
		let environmentId = requestedId;
		if (requestedId === 'all' && prunableEnvironments.length === 1 && onlyEnvironment) {
			environmentId = onlyEnvironment.environment.id;
		}
		const targetIds = prunableEnvironments
			.map((item) => item.environment.id)
			.filter((id) => environmentId === 'all' || id === environmentId);
		if (targetIds.length === 0) {
			return;
		}

		pruneTargetId = environmentId;
		pruneDefaultsLoadingId = environmentId;
		try {
			const settings = await tryCatch(
				readEachEnvironment(
					prunableEnvironments.map((item) => item.environment).filter((env) => targetIds.includes(env.id)),
					(scope, config) => settingsService.getSettingsForEnvironment(scope, config),
					emptySettings
				)
			);
			if (settings.error !== null || settings.data.failedEnvironments.length > 0) {
				pruneDefaults = null;
			} else {
				const [first, ...rest] = settings.data.byEnvironment.map(({ data }) => data.values);
				pruneDefaults = Object.fromEntries(
					PRUNE_SETTING_GROUPS.filter((group) =>
						group.every((key) => rest.every((values) => values[key] === first?.[key]))
					).flatMap((group) => group.map((key) => [key, first?.[key]]))
				) as Partial<Settings>;
			}
		} finally {
			pruneDefaultsLoadingId = null;
		}

		if (pruneTargetId === environmentId) {
			isPruneDialogOpen = true;
		}
	}

	function closePruneDialog() {
		if (pruningEnvironmentId) {
			return;
		}

		isPruneDialogOpen = false;
		pruneTargetId = null;
		pruneDefaults = null;
	}

	async function confirmPrune(pruneRequest: PruneAllRequest) {
		const selectedTypes = Object.keys(pruneRequest) as PruneType[];
		const targetId = pruneTargetId;
		const targets = prunableEnvironments.filter((item) => targetId === 'all' || item.environment.id === targetId);
		if (!targetId || pruningEnvironmentId || selectedTypes.length === 0 || targets.length === 0) {
			return;
		}

		const typeLabels: Record<PruneType, string> = {
			containers: m.prune_stopped_containers(),
			images: m.prune_unused_images(),
			networks: m.unused_networks(),
			volumes: m.unused_volumes(),
			buildCache: m.build_cache()
		};
		const typesString = selectedTypes.map((type) => typeLabels[type]).join(', ');

		let successMessage = m.dashboard_prune_success_many({ types: typesString });
		if (selectedTypes.length === 1) {
			successMessage = m.dashboard_prune_success_one({ types: typesString });
		}

		pruningEnvironmentId = targetId;

		const [target] = targets;
		if (targets.length === 1 && target) {
			await handleApiResultWithCallbacks({
				result: await tryCatch(systemService.pruneAllForEnvironment(target.environment.id, pruneRequest)),
				message: m.dashboard_prune_failed({ types: typesString }),
				setLoadingState: (value) => {
					pruningEnvironmentId = value ? targetId : null;
				},
				onSuccess: (data) => {
					isPruneDialogOpen = false;
					pruneTargetId = null;
					pruneDefaults = null;
					toast.success(successMessage, {
						...activityToastOptions(extractActivityId(data)),
						description: target.environment.name
					});
					void diskUsageQuery.refetch();
				}
			});
			return;
		}

		const result = await tryCatch(
			readEachEnvironment(
				targets.map((target) => target.environment),
				(scope, config) => systemService.pruneAllForEnvironment(scope, pruneRequest, config),
				{}
			)
		);
		pruningEnvironmentId = null;
		isPruneDialogOpen = false;
		pruneTargetId = null;
		pruneDefaults = null;
		void diskUsageQuery.refetch();

		let succeeded = 0;
		let failures = [extractApiErrorMessage(result.error)];
		if (result.error === null) {
			succeeded = result.data.byEnvironment.length;
			failures = result.data.failedEnvironments.map(({ environment, message }) => `${environment.name}: ${message}`);
		}
		if (succeeded > 0) {
			toast.success(successMessage, {
				description: m.across_count_environments({ count: succeeded })
			});
		}
		if (failures.length > 0) {
			toast.error(m.dashboard_prune_failed({ types: typesString }), { description: failures.join('\n') });
		}
	}
</script>

{#snippet environmentCard(environment: Environment)}
	{@const overview = overviewById.get(environment.id) ?? createBaseEnvironmentOverview(environment)}
	<svelte:boundary>
		<EnvironmentCard
			{overview}
			systemStats={liveStats.get(environment.id)?.stats ?? null}
			liveStatsStatus={liveStats.status(environment)}
			countsLoading={isEnvironmentCountsLoading(environment.id)}
			attentionCount={attentionCountByEnvironment.get(environment.id) ?? 0}
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
					<ArcaneButton action="refresh" customLabel={m.common_retry()} onclick={reset} />
				</div>
			</Alert.Root>
		{/snippet}
	</svelte:boundary>
{/snippet}

<div class="flex flex-col gap-4 pb-6">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<h1 class="min-w-0 text-2xl font-semibold tracking-tight">{heroGreeting}</h1>

		<div class="flex shrink-0 flex-wrap items-center justify-end gap-2">
			{#if editingLayout}
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({ props })}
							<ArcaneButton {...props} action="base" icon={AddIcon} customLabel={m.add_widget()} />
						{/snippet}
					</DropdownMenu.Trigger>
					<DropdownMenu.Content align="end" class="max-w-64">
						{#if hiddenWidgets.length > 0}
							<DropdownMenu.Group>
								{#each hiddenWidgets as widget (widget.id)}
									<DropdownMenu.Item onclick={() => (draftLayout = showWidget(draftLayout, widget.id))}>
										{DASHBOARD_WIDGET_TYPES[widget.type].label()}
									</DropdownMenu.Item>
								{/each}
							</DropdownMenu.Group>
							<DropdownMenu.Separator />
						{/if}
						<DropdownMenu.Item disabled={widgetLimitReached} onclick={() => widgetSheet?.show()}>
							{m.new_widget_ellipsis()}
						</DropdownMenu.Item>
						{#if widgetLimitReached}
							<p class="px-2 py-1.5 text-xs text-muted-foreground">
								{m.widget_limit_reached({ max: MAX_DASHBOARD_WIDGETS })}
							</p>
						{/if}
					</DropdownMenu.Content>
				</DropdownMenu.Root>
				<ArcaneButton
					action="base"
					icon={ResetIcon}
					customLabel={m.reset_to_default()}
					onclick={() => (draftLayout = defaultDashboardLayout(environmentCards.length, environmentsLayout))}
				/>
				{#if layoutHasChanges}
					<span class="mx-1 text-xs text-warning-foreground">{m.common_unsaved_changes()}</span>
					<ArcaneButton
						action="base"
						tone="outline"
						class="gap-2"
						icon={ResetIcon}
						customLabel={m.common_reset()}
						onclick={() => (draftLayout = savedLayout)}
					/>
				{/if}
				<ArcaneButton action="cancel" onclick={() => (editingLayout = false)} />
				<ArcaneButton
					action="save"
					class="min-w-20 gap-2"
					disabled={!layoutHasChanges}
					onClickPromise={async () => {
						await saveUserPreferences({ dashboardLayout: draftLayout });
						editingLayout = false;
					}}
				/>
			{:else}
				<ArcaneTooltip.Root>
					<ArcaneTooltip.Trigger>
						{#snippet child({ props })}
							<ArcaneButton
								{...props}
								action="refresh"
								tone="outline"
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
								tone="outline"
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
			<Alert.Root variant="warning-subtle" size="sm" icon={AlertIcon}>
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
					<ArcaneButton action="refresh" customLabel={m.common_retry()} onClickPromise={refetchEnvironmentQueries} />
				</div>
			</Alert.Root>
		</section>
	{/if}

	{#if excludedEnvironmentCount > 0}
		<section>
			<Alert.Root variant="warning-subtle" size="sm" icon={AlertIcon}>
				<Alert.Description>
					{m.all_environments_excluded({
						count: excludedEnvironmentCount,
						limit: settingsStore.current?.allEnvironmentsLimit ?? 0
					})}
				</Alert.Description>
			</Alert.Root>
		</section>
	{/if}

	<WidgetGrid
		{layout}
		editing={editingLayout}
		environmentsView={boardView}
		onChange={(next) => (draftLayout = next)}
		onConfigure={(widget) => widgetSheet?.show(widget)}
	>
		{#snippet widget(item)}
			{#if item.type === 'healthy' || item.type === 'attentionCount' || item.type === 'updates' || item.type === 'risk' || item.type === 'reclaimableTotal'}
				<StatWidget
					id={item.type}
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
						loading: loadableEnvironmentIds.size > 0 && diskUsageQuery.isPending,
						failedEnvironments: diskUsageFailures
					}}
				/>
			{:else if item.type === 'environments'}
				<DashboardPanel title={m.environments_title()}>
					{#snippet actions()}
						{#if !isMobile.current}
							<div class="inline-flex items-center gap-0.5 rounded-lg border border-border/60 bg-muted/40 p-0.5">
								{#each [{ view: 'cards' as const, icon: LayoutGridIcon, label: m.dashboard_view_cards() }, { view: 'table' as const, icon: LayoutListIcon, label: m.dashboard_view_table() }] as option (option.view)}
									<button
										type="button"
										title={option.label}
										aria-label={option.label}
										aria-pressed={boardView === option.view}
										class={cn(
											'inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
											boardView === option.view && 'bg-primary/15 text-primary-text ring-1 ring-primary/30'
										)}
										onclick={() => (boardViewPref.current = option.view)}
									>
										<option.icon class="size-4" />
									</button>
								{/each}
							</div>
						{/if}
					{/snippet}
					{#if environmentCards.length === 0}
						<div class="rounded-xl border border-dashed border-border/60 px-4 py-8 text-center">
							<p class="text-sm text-muted-foreground">{m.dashboard_all_no_visible_environments()}</p>
							{#if isLocalDockerDisabled(environmentStore.getLocalEnvironment())}
								<a href={resolve('/(app)/agents')} class="mt-2 inline-block text-sm underline underline-offset-4">
									{m.local_docker_disabled_add_agent()}
								</a>
							{/if}
						</div>
					{:else if boardView === 'table'}
						<EnvironmentsTable
							rows={environmentTableRows}
							getLiveStatsState={(environmentId) => liveStats.get(environmentId)}
							getLiveStatsStatus={(environment) => liveStats.status(environment)}
						/>
					{:else}
						<div class="grid grid-cols-1 gap-4 max-md:gap-0 max-md:divide-y max-md:divide-border/60 xl:grid-cols-2">
							{#each environmentCards as item (item.environment.id)}
								{@render environmentCard(item.environment)}
							{/each}
						</div>
					{/if}
				</DashboardPanel>
			{:else if item.type === 'attention'}
				<AttentionList
					items={attentionItems}
					loading={boardSummaryLoading}
					failed={attentionFailed}
					onAction={handleAttentionAction}
				/>
			{:else if item.type === 'activity'}
				<ActivityList groups={recentActivityGroups} loading={activityStore.loading} unavailable={activityStore.streamError} />
			{:else if item.type === 'jobs'}
				<UpcomingJobs
					jobs={environmentJobs}
					loading={jobsQuery.isPending && loadableEnvironmentIds.size > 0}
					failed={jobsQuery.isError}
				/>
			{:else if item.type === 'reclaimable'}
				<ReclaimableSpace
					usage={diskUsage}
					loading={diskUsageQuery.isPending && loadableEnvironmentIds.size > 0}
					failed={diskUsageQuery.isError}
					failedEnvironments={diskUsageFailures}
					onPrune={prunableEnvironments.length > 0 ? () => void openPruneDialog('all') : undefined}
				/>
			{:else if item.type === 'memory'}
				<TopMemory
					containers={topMemoryQuery.data?.data ?? []}
					loading={topMemoryQuery.isPending && loadableEnvironmentIds.size > 0}
					failed={topMemoryQuery.isError}
				/>
			{:else}
				<CustomWidget widget={item} {environmentCard} />
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

<WidgetSheet
	bind:this={widgetSheet}
	onSave={async (widget) => {
		const { id } = widget;
		if (id) {
			draftLayout = draftLayout.map((entry) => (entry.id === id ? { ...widget, id } : entry));
			return;
		}
		await saveUserPreferences({ dashboardLayout: addDashboardWidget(draftLayout, widget) });
		draftLayout = savedLayout;
	}}
/>

<PruneConfirmationDialog
	open={isPruneDialogOpen}
	isPruning={!!pruningEnvironmentId}
	defaults={pruneDefaults}
	onConfirm={confirmPrune}
	onCancel={closePruneDialog}
	environments={prunableEnvironments.map(({ environment }) => ({ id: environment.id, name: environment.name }))}
	environmentId={pruneTargetId}
	onEnvironmentChange={(environmentId) => void openPruneDialog(environmentId)}
/>
