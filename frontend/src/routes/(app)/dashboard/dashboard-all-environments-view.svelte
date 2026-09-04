<script lang="ts">
	import { goto, refreshAll } from '$app/navigation';
	import { onDestroy, onMount, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { type ActionButton } from '#lib/components/action-button-group/index.js';
	import { cn } from '#lib/utils';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import PruneConfirmationDialog from '#lib/components/dialogs/prune-confirmation-dialog.svelte';
	import DockerInfoDialog from '#lib/components/dialogs/docker-info-dialog.svelte';
	import { Skeleton } from '#lib/components/ui/skeleton';
	import { m } from '#lib/paraglide/messages';
	import { settingsService } from '#lib/services/settings-service';
	import { systemService } from '#lib/services/system-service';
	import { activityStore } from '#lib/stores/activity.store.svelte';
	import { dashboardStore } from '#lib/stores/dashboard.store.svelte';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import userStore from '#lib/stores/user-store';
	import { hasAnyPermission, hasPermission } from '#lib/utils/auth';
	import type {
		DashboardActionItemKind,
		DashboardEnvironmentCardState,
		DashboardEnvironmentOverview,
		SystemStats
	} from '#lib/types/shared';
	import type { Environment } from '#lib/types/environment';
	import type { DockerInfo } from '#lib/types/docker';
	import type { PruneType, SystemPruneRequest } from '#lib/types/automation';
	import type { AppVersionInformation, Settings } from '#lib/types/settings';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api';
	import { tryCatch } from '#lib/utils/api';
	import { isEnvironmentOnline } from '#lib/utils/docker';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast';
	import { createStatsWebSocket, type ReconnectingWebSocket } from '#lib/utils/ws';
	import {
		ContainersIcon,
		EnvironmentsIcon,
		ImagesIcon,
		InfoIcon,
		InspectIcon,
		RefreshIcon,
		TrashIcon,
		UpdateIcon,
		VolumesIcon,
		VerifiedCheckIcon,
		LayoutGridIcon,
		LayoutListIcon
	} from '#lib/icons';
	import DashboardEnvironmentsTable, { type EnvironmentTableRow } from './dashboard-environments-table.svelte';
	import DashboardEnvironmentCard from './dashboard-environment-card.svelte';
	import { PersistedState } from 'runed';
	import {
		buildOverviewSummary,
		createBaseEnvironmentOverview,
		formatContainerOverviewLabel,
		formatImageOverviewLabel,
		formatVolumeOverviewLabel,
		getCpuMetric,
		getDiskMetric,
		getMemoryMetric,
		shouldLoadEnvironment
	} from './dashboard-overview';

	let {
		heroGreeting,
		debugAllGood = false,
		debugUpgrade = false
	}: {
		heroGreeting: string;
		debugAllGood?: boolean;
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

	const availableEnvironments = $derived.by(() => {
		if (!$userStore) {
			return [];
		}

		return environmentStore.available.filter((environment) => hasPermission('dashboard:read', environment.id));
	});
	const currentEnvironmentId = $derived(environmentStore.selected?.id ?? null);

	function canPruneInEnvironment(envId: string): boolean {
		return hasAnyPermission(['images:prune', 'volumes:prune', 'networks:prune'], envId);
	}
	function canUpgradeEnvironment(): boolean {
		return hasPermission('environments:update');
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
		return [...availableEnvironments]
			.sort((a, b) => {
				const currentOrder = Number(b.id === currentEnvironmentId) - Number(a.id === currentEnvironmentId);
				if (currentOrder !== 0) {
					return currentOrder;
				}

				return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.id.localeCompare(b.id);
			})
			.map((environment) => ({ environment }));
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
					volumeUsageCounts: snapshot.volumeUsageCounts,
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
			summary: buildOverviewSummary(items)
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
			await refreshAll();
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

	function getLiveStatsState(environmentId: string): EnvironmentLiveStatsState | null {
		return liveStatsByEnvironmentId[environmentId] ?? null;
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
			onclick: () => void goto(`/environments/${item.environment.id}`),
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

	// Board view mode: cards or table. 'auto' resolves to table once the board
	// grows past a handful of environments (tiles stop scaling — #2778).
	const boardViewPref = new PersistedState<'auto' | 'cards' | 'table'>('dashboard-environments-view', 'auto');
	const boardView = $derived.by((): 'cards' | 'table' => {
		if (boardViewPref.current !== 'auto') return boardViewPref.current;
		return environmentCards.length > 6 ? 'table' : 'cards';
	});

	const environmentTableRows = $derived.by((): EnvironmentTableRow[] => {
		return environmentCards.map(({ environment }) => {
			const baseItem = createBaseEnvironmentOverview(environment);
			const loadedItem = boardState.overviewById.get(environment.id) ?? baseItem;
			const stats = getLiveStatsState(environment.id)?.stats ?? null;
			const isCurrent = currentEnvironmentId === environment.id;
			const [useButton, ...menuButtons] = getEnvironmentActionButtons(loadedItem, isCurrent);
			const vInfo = loadedItem.versionInfo;
			const actionItemCount = (kind: DashboardActionItemKind) =>
				loadedItem.actionItems.items.find((item) => item.kind === kind)?.count ?? 0;
			return {
				environment,
				isCurrent,
				online: isEnvironmentOnline(environment),
				loading: isEnvironmentSnapshotLoading(environment.id),
				running: loadedItem.containers.runningContainers,
				total: loadedItem.containers.totalContainers,
				images: loadedItem.imageUsageCounts.totalImages,
				updates: actionItemCount('image_updates'),
				vulnerabilities: actionItemCount('actionable_vulnerabilities'),
				cpu: getCpuMetric(stats),
				memory: getMemoryMetric(stats),
				disk: getDiskMetric(stats),
				versionText: vInfo ? vInfo.displayVersion || vInfo.currentTag || vInfo.currentVersion || 'unknown' : null,
				updateAvailable: !!vInfo?.updateAvailable,
				useButton,
				menuButtons
			};
		});
	});

	// Updates hero: aggregate pending image updates across every environment,
	// mirroring the iOS Updates overview header.
	const updatesOverview = $derived.by(() => {
		let pending = 0;
		let checkedEnvironments = 0;

		for (const { environment } of environmentCards) {
			if (!shouldLoadEnvironment(environment)) {
				continue;
			}
			const state = dashboardStore.getEnvironmentState(environment.id);
			if (!state?.hasLoaded) {
				continue;
			}
			checkedEnvironments += 1;
			if (debugAllGood) {
				continue;
			}
			const updateItem = state.snapshot?.actionItems.items.find((item) => item.kind === 'image_updates');
			pending += updateItem?.count ?? 0;
		}

		const checking = checkedEnvironments < loadableEnvironmentCards.length;
		return { pending, checking };
	});

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
			networks: m.unused_networks(),
			volumes: m.unused_volumes(),
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

<div class="flex h-full min-h-0 flex-col gap-4 overflow-hidden pt-3 md:gap-5 md:pt-5">
	<header class="flex shrink-0 items-start justify-between gap-4">
		<div class="min-w-0 space-y-1">
			<p class="text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">{m.dashboard_title()}</p>
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
							boardView === option.view &&
								'bg-primary/15 text-primary ring-1 ring-primary/30 dark:text-[color-mix(in_oklch,var(--primary)_55%,white)]'
						)}
						onclick={() => (boardViewPref.current = option.view)}
					>
						<option.icon class="size-4" />
					</button>
				{/each}
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

	<section class="shrink-0">
		{#if boardSummaryLoading}
			<div class="grid grid-cols-2 gap-x-6 gap-y-4 lg:grid-cols-4">
				{#each [{ icon: UpdateIcon, label: m.updates() }, { icon: ContainersIcon, label: m.containers() }, { icon: ImagesIcon, label: m.images() }, { icon: VolumesIcon, label: m.resource_volumes_cap() }] as tile (tile.label)}
					<div class="min-w-0">
						<div class="flex items-center gap-1.5 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">
							<tile.icon class="size-3.5" />
							<span>{tile.label}</span>
						</div>
						<Skeleton class="mt-1.5 h-7 w-12" />
						<Skeleton class="mt-1.5 h-3.5 w-28" />
					</div>
				{/each}
			</div>
		{:else}
			{@const summary = boardState.summary}
			<div class="grid grid-cols-2 gap-x-6 gap-y-4 lg:grid-cols-4">
				<button
					type="button"
					onclick={() => goto('/updates')}
					class="group min-w-0 cursor-pointer rounded-sm text-left transition-colors focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring"
				>
					<div
						class="flex items-center gap-1.5 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase transition-colors group-hover:text-foreground"
					>
						<UpdateIcon class="size-3.5" />
						<span>{m.updates()}</span>
					</div>
					<div class="mt-1 text-2xl font-semibold tracking-tight tabular-nums">{updatesOverview.pending}</div>
					<div class="mt-0.5 flex h-4 items-center gap-1.5 truncate text-xs text-muted-foreground">
						{#if updatesOverview.pending === 0 && !updatesOverview.checking}
							<VerifiedCheckIcon class="size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />
							<span class="truncate">{m.dashboard_updates_up_to_date()}</span>
						{:else if updatesOverview.checking}
							<span class="truncate">{m.dashboard_updates_checking()}</span>
						{:else}
							<span class="truncate">{m.dashboard_updates_available_label()}</span>
						{/if}
					</div>
				</button>

				<button
					type="button"
					onclick={() => goto('/containers')}
					class="group min-w-0 cursor-pointer rounded-sm border-border/60 text-left transition-colors focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring max-lg:border-l max-lg:pl-6 lg:border-l lg:pl-6"
				>
					<div
						class="flex items-center gap-1.5 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase transition-colors group-hover:text-foreground"
					>
						<ContainersIcon class="size-3.5" />
						<span>{m.containers()}</span>
					</div>
					<div class="mt-1 text-2xl font-semibold tracking-tight tabular-nums">{summary.totalContainers}</div>
					<div class="mt-0.5 truncate text-xs text-muted-foreground">{formatContainerOverviewLabel(summary)}</div>
				</button>

				<button
					type="button"
					onclick={() => goto('/images')}
					class="group min-w-0 cursor-pointer rounded-sm border-border/60 text-left transition-colors focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring lg:border-l lg:pl-6"
				>
					<div
						class="flex items-center gap-1.5 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase transition-colors group-hover:text-foreground"
					>
						<ImagesIcon class="size-3.5" />
						<span>{m.images()}</span>
					</div>
					<div class="mt-1 text-2xl font-semibold tracking-tight tabular-nums">{summary.totalImages}</div>
					<div class="mt-0.5 truncate text-xs text-muted-foreground">{formatImageOverviewLabel(summary)}</div>
				</button>

				<button
					type="button"
					onclick={() => goto('/volumes')}
					class="group min-w-0 cursor-pointer rounded-sm border-border/60 text-left transition-colors focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring max-lg:border-l max-lg:pl-6 lg:border-l lg:pl-6"
				>
					<div
						class="flex items-center gap-1.5 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase transition-colors group-hover:text-foreground"
					>
						<VolumesIcon class="size-3.5" />
						<span>{m.resource_volumes_cap()}</span>
					</div>
					<div class="mt-1 text-2xl font-semibold tracking-tight tabular-nums">{summary.totalVolumes}</div>
					<div class="mt-0.5 truncate text-xs text-muted-foreground">{formatVolumeOverviewLabel(summary)}</div>
				</button>
			</div>
		{/if}
	</section>

	<section class="flex min-h-0 flex-1 flex-col overflow-hidden border-t border-border/60 pt-3">
		{#if environmentCards.length === 0}
			<div class="rounded-xl border border-dashed border-border/60 px-4 py-8 text-center">
				<p class="text-sm text-muted-foreground">{m.dashboard_all_no_visible_environments()}</p>
			</div>
		{:else if boardView === 'table'}
			<div class="min-h-0 flex-1 overflow-y-auto pb-2">
				<DashboardEnvironmentsTable rows={environmentTableRows} />
			</div>
		{:else}
			<div class="min-h-0 flex-1 overflow-y-auto pb-2">
				<div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
					{#each environmentCards as item (item.environment.id)}
						{@const baseItem = createBaseEnvironmentOverview(item.environment)}
						{@const environment = baseItem.environment}
						{@const overview = boardState.overviewById.get(environment.id) ?? baseItem}
						{@const isCurrent = currentEnvironmentId === environment.id}
						{@const liveStatsState = getLiveStatsState(environment.id)}
						{@const systemStats = liveStatsState?.stats ?? null}
						{@const liveStatsLoading = liveStatsState?.loading ?? shouldLoadEnvironment(environment)}
						{@const [useButton, ...menuButtons] = getEnvironmentActionButtons(overview, isCurrent)}
						<DashboardEnvironmentCard
							{overview}
							{isCurrent}
							{systemStats}
							{liveStatsLoading}
							snapshotLoading={isEnvironmentSnapshotLoading(environment.id)}
							{useButton}
							{menuButtons}
							{debugUpgrade}
							debugVersionInfo={DEBUG_VERSION_INFO}
							canUpgrade={canUpgradeEnvironment()}
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

<DockerInfoDialog bind:open={dockerInfoOpen} dockerInfo={dockerInfoData} {dockerInfoPromise} errorMessage={dockerInfoError} />
