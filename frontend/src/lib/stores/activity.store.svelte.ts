import {
	discardPendingActivityToasts,
	queueActivityCompletionToast
} from '#lib/components/activity/activity-completion-toasts.js';
import { activityService } from '#lib/services/activity-service.js';
import { STREAM_CHANNEL_ACTIVITIES } from '#lib/services/stream-service.js';
import {
	createEnvironmentStreamStore,
	environmentDisplayName,
	streamErrorMessage,
	type StreamEnvStateBase
} from '#lib/stores/environment-stream.svelte.js';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import type {
	Activity,
	ActivityDetail,
	ActivityMessage,
	ActivityStatus,
	ActivityStreamEvent,
	ActivityType
} from '#lib/types/activity.js';
import type { Environment } from '#lib/types/environment.js';
import type {
	ActivityBatchGroup,
	ActivityClearHistorySummary,
	ActivityEnvironmentFailure,
	ActivityGroup
} from '#lib/types/ui/activity.js';
import { extractApiErrorMessage } from '#lib/utils/api.js';
import { limitEnvironments, readAcrossEnvironments, type EnvironmentFailure } from '#lib/utils/environments.js';
import { instantEpochMilliseconds, parseInstant } from '#lib/utils/formatting.js';
import { tryCatch } from '#lib/utils/try-catch.js';

const ACTIVITY_LIST_LIMIT = 50;
const ACTIVITY_DETAIL_LIMIT = 500;

type ActivityEnvironmentState = StreamEnvStateBase & {
	activities: Activity[];
};

function sortActivities(items: Activity[]): Activity[] {
	const sortTimeMs = new Map(items.map((a) => [a, getActivitySortTime(a)]));
	return [...items].sort((a, b) => {
		const aActive = isActiveStatus(a.status);
		const bActive = isActiveStatus(b.status);
		if (aActive !== bActive) return aActive ? -1 : 1;
		const diff = (sortTimeMs.get(b) ?? 0) - (sortTimeMs.get(a) ?? 0);
		if (diff !== 0) return diff;
		return b.id.localeCompare(a.id);
	});
}

// Mirrors the backend default order: active rows keep their immutable createdAt
// so progress updates never reshuffle them; terminal rows sort by endedAt.
function getActivitySortTime(activity: Activity): number {
	const value = activity.endedAt || activity.createdAt || activity.startedAt;
	return instantEpochMilliseconds(value) ?? 0;
}

function isActiveStatus(status: ActivityStatus): boolean {
	return status === 'queued' || status === 'running';
}

function activitySearchHaystack(activity: Activity): string {
	// Mirrors the backend LIKE search fields, plus the environment name.
	return [
		activity.type,
		activity.resourceName,
		activity.resourceId,
		activity.latestMessage,
		activity.error,
		activity.sourceEnvironmentName
	]
		.filter(Boolean)
		.join('\n')
		.toLowerCase();
}

function groupActivities(items: Activity[]): ActivityGroup[] {
	const batchItems = Map.groupBy(
		items.filter((activity) => activity.batchId),
		(activity) => activity.batchId
	);
	return items.flatMap((activity): ActivityGroup[] => {
		const members = activity.batchId ? batchItems.get(activity.batchId) : undefined;
		if (!activity.batchId || !members || members.length === 1) {
			return [{ kind: 'single', activity }];
		}
		if (members[0] !== activity) {
			return [];
		}
		return [batchGroup(activity.batchId, members)];
	});
}

function batchGroup(batchId: string, items: Activity[]): ActivityBatchGroup {
	const jobActivity = items.find((item) => item.type === 'job_run');
	const operations = jobActivity ? items.filter((item) => item.type !== 'job_run') : items;
	const failed = operations.filter((item) => item.status === 'failed').length;
	return {
		kind: 'batch',
		batchId,
		items,
		total: operations.length,
		done: operations.filter((item) => !isActiveStatus(item.status)).length,
		failed,
		status: jobActivity?.status ?? batchStatus(items, failed)
	};
}

function batchStatus(items: Activity[], failed: number): ActivityStatus {
	if (items.some((item) => isActiveStatus(item.status))) {
		return items.some((item) => item.status === 'running') ? 'running' : 'queued';
	}
	if (failed > 0) {
		return 'failed';
	}
	if (items.every((item) => item.status === 'cancelled')) {
		return 'cancelled';
	}
	return 'success';
}

function groupStatusIsActive(group: ActivityGroup): boolean {
	return group.kind === 'batch' ? isActiveStatus(group.status) : isActiveStatus(group.activity.status);
}

function activeUpdateKeys(activities: readonly Activity[]): Set<string> {
	return new Set(
		activities
			.filter(
				(activity) => activity.type === 'auto_update' && activity.resourceType === 'container' && isActiveStatus(activity.status)
			)
			.map((activity) => `${activity.sourceEnvironmentId || activity.environmentId}:${activity.resourceId}`)
	);
}

function omitKey<T>(record: Record<string, T>, key: string): Record<string, T> {
	return Object.fromEntries(Object.entries(record).filter(([entryKey]) => entryKey !== key));
}

type ActivityStoreState = {
	environmentActivities: Record<string, Activity[]>;
	details: Record<string, ActivityDetail>;
	expandedActivityIds: Record<string, boolean>;
	detailLoadingIds: Record<string, boolean>;
	detailErrors: Record<string, string>;
	cancellingIds: Record<string, boolean>;
	expandedBatchIds: Record<string, boolean>;
	searchTerm: string;
	statusFilters: ActivityStatus[];
	typeFilters: ActivityType[];
	open: boolean;
	focusedKey: string | null;
};

function initialState(): ActivityStoreState {
	return {
		environmentActivities: {},
		details: {},
		expandedActivityIds: {},
		detailLoadingIds: {},
		detailErrors: {},
		cancellingIds: {},
		expandedBatchIds: {},
		searchTerm: '',
		statusFilters: [],
		typeFilters: [],
		open: false,
		focusedKey: null
	};
}

function sourceEnvironmentId(activity: Activity | null | undefined): string {
	return activity?.sourceEnvironmentId || activity?.environmentId || LOCAL_DOCKER_ENVIRONMENT_ID;
}

function createActivityStore() {
	let state = $state.raw<ActivityStoreState>(initialState());
	let _activities = $state.raw<Activity[]>([]);
	let _activeUpdateKeys = $state.raw<ReadonlySet<string>>(new Set());
	let session = new AbortController();
	let requestedEnvironments: Record<string, string> = {};
	const observedStatusById = new Map<string, ActivityStatus>();
	const completionToastQueuedIds = new Set<string>();
	const activitySubscribers = new Set<(activities: readonly Activity[]) => void>();

	function setActivities(activities: Activity[]) {
		_activities = activities;
		const keys = activeUpdateKeys(activities);
		if (keys.size !== _activeUpdateKeys.size || [...keys].some((key) => !_activeUpdateKeys.has(key))) {
			_activeUpdateKeys = keys;
		}
	}

	function notifyActivitySubscribers() {
		for (const subscriber of activitySubscribers) subscriber(_activities);
	}

	function noteActivityStatus(activity: Activity) {
		const prev = observedStatusById.get(activity.id);
		observedStatusById.set(activity.id, activity.status);
		if (!prev || !isActiveStatus(prev) || prev === activity.status || completionToastQueuedIds.has(activity.id)) {
			return;
		}
		if (activity.status !== 'success' && activity.status !== 'failed') {
			return;
		}
		if (state.open) {
			return;
		}
		const currentUserId = userStore.current?.id;
		if (!activity.startedBy?.userId || !currentUserId || activity.startedBy.userId !== currentUserId) {
			return;
		}
		completionToastQueuedIds.add(activity.id);
		queueActivityCompletionToast(activity, openCenter);
	}

	const core = createEnvironmentStreamStore<ActivityEnvironmentState, ActivityStreamEvent>({
		label: 'Activity',
		includeEnvironment: (environment) => userStore.hasPermission('activities:list', environment.id),
		watchEnvironmentFilter: () => userStore.current,
		createEnvironmentState(environment: Pick<Environment, 'id' | 'name'>): ActivityEnvironmentState {
			return {
				id: environment.id || LOCAL_DOCKER_ENVIRONMENT_ID,
				name: environmentDisplayName(environment),
				activities: [],
				loading: true,
				streamError: false
			};
		},
		channel: STREAM_CHANNEL_ACTIVITIES,
		refreshOnStart: true,
		applyEvent(environmentId, event) {
			switch (event.type) {
				case 'snapshot':
					replaceEnvironmentSnapshot(environmentId, event.activities ?? []);
					break;
				case 'activity':
					if (event.activity) {
						mergeActivity(normalizeActivity(event.activity, environmentId));
					}
					break;
				case 'message':
					if (event.message) {
						mergeMessage(event.message);
					}
					break;
				case 'error':
					core.setEnvironmentError(environmentId, new Error(event.error || 'Activity stream error'));
					break;
			}
		},
		fetchSnapshots,
		onEnvironmentRemoved(environmentId) {
			state = {
				...state,
				environmentActivities: omitKey(state.environmentActivities, environmentId)
			};
			rebuildActivities();
		},
		onEnvironmentRenamed(environmentId, name) {
			const existing = core.environmentState(environmentId);
			const updatedActivities = (state.environmentActivities[environmentId] ?? existing?.activities ?? []).map((activity) => {
				if (activity.sourceEnvironmentName) return activity;
				return { ...activity, sourceEnvironmentName: name };
			});
			state = {
				...state,
				environmentActivities: {
					...state.environmentActivities,
					[environmentId]: updatedActivities
				}
			};
			core.updateEnvironmentState(environmentId, (environmentState) => ({
				...environmentState,
				name,
				activities: updatedActivities
			}));
			rebuildActivities();
		}
	});

	function normalizeActivity(activity: Activity, environmentId: string): Activity {
		if (activity.type === 'job_run') {
			const current = state.details[activity.id]?.activity ?? _activities.find((item) => item.id === activity.id);
			const currentTime = parseInstant(current?.updatedAt);
			const incomingTime = parseInstant(activity.updatedAt);
			if (current && currentTime && (!incomingTime || incomingTime.epochNanoseconds <= currentTime.epochNanoseconds))
				return current;
		}
		const environmentState = core.environmentState(environmentId);
		return {
			...activity,
			sourceEnvironmentId: activity.sourceEnvironmentId || environmentId,
			sourceEnvironmentName: activity.sourceEnvironmentName || environmentState?.name || environmentId
		};
	}

	async function fetchSnapshots(requestedIds: string[], signal: AbortSignal): Promise<void> {
		const environmentIds = requestedIds.filter((id) => core.environmentState(id));
		if (environmentIds.length === 0) return;
		for (const environmentId of environmentIds) {
			core.updateEnvironmentState(environmentId, (environmentState) => ({ ...environmentState, loading: true }));
		}
		const { included, excluded } = limitEnvironments(
			environmentIds.map((id) => ({ id, name: core.environmentState(id)?.name ?? id }))
		);
		let reported: EnvironmentFailure[] = [];
		const result = await tryCatch(
			readAcrossEnvironments(
				included,
				(scope, config) =>
					activityService.getActivities({ pagination: { page: 1, limit: ACTIVITY_LIST_LIMIT } }, scope, {
						...config,
						onFailedEnvironments: (failures) => {
							reported = failures;
							config.onFailedEnvironments?.(failures);
						}
					}),
				false
			)
		);
		if (signal.aborted) return;

		if (result.error !== null) {
			for (const environment of included) {
				const failure = reported.find((entry) => entry.environment.id === environment.id);
				setSnapshotError(environment.id, failure ? new Error(failure.message) : result.error);
			}
		} else {
			const rows = result.data.value.data ?? [];
			for (const environment of included) {
				const failure = result.data.failures.find((entry) => entry.environment.id === environment.id);
				if (failure) {
					setSnapshotError(environment.id, new Error(failure.message));
					continue;
				}
				const environmentRows = included.length > 1 ? rows.filter((row) => row.environmentId === environment.id) : rows;
				replaceEnvironmentSnapshot(environment.id, environmentRows);
			}
		}

		await fetchSnapshots(
			excluded.map((environment) => environment.id),
			signal
		);
	}

	function setSnapshotError(environmentId: string, error: unknown) {
		if (!core.environmentState(environmentId)) return;
		console.warn('Failed to refresh activities:', error);
		core.setEnvironmentError(environmentId, error);
	}

	function replaceEnvironmentSnapshot(environmentId: string, activities: Activity[]) {
		if (!core.environmentState(environmentId)) {
			return;
		}
		const normalizedActivities = sortActivities(activities.map((activity) => normalizeActivity(activity, environmentId)));
		let details = state.details;
		for (const activity of normalizedActivities) {
			noteActivityStatus(activity);
			const detail = details[activity.id];
			if (activity.type === 'job_run' && detail) details = { ...details, [activity.id]: { ...detail, activity } };
		}
		state = {
			...state,
			details,
			environmentActivities: {
				...state.environmentActivities,
				[environmentId]: normalizedActivities
			}
		};
		core.updateEnvironmentState(environmentId, (environmentState) => ({
			...environmentState,
			activities: normalizedActivities,
			loading: false,
			streamError: false,
			errorMessage: undefined
		}));
		rebuildActivities();
	}

	function rebuildActivities() {
		const activities = sortActivities(Object.values(state.environmentActivities).flat());
		setActivities(activities);

		const present = new Set(activities.map((activity) => activity.id));
		state = {
			...state,
			expandedActivityIds: Object.fromEntries(
				Object.entries(state.expandedActivityIds)
					.filter(([id, expanded]) => expanded && present.has(id))
					.map(([id]) => [id, true])
			)
		};

		for (const id of observedStatusById.keys()) {
			if (!present.has(id)) {
				observedStatusById.delete(id);
				completionToastQueuedIds.delete(id);
			}
		}
		notifyActivitySubscribers();
	}

	function mergeActivity(activity: Activity) {
		const environmentId = sourceEnvironmentId(activity);
		const normalized = normalizeActivity(activity, environmentId);
		noteActivityStatus(normalized);
		const currentActivities =
			state.environmentActivities[environmentId] ?? core.environmentState(environmentId)?.activities ?? [];
		const index = currentActivities.findIndex((item) => item.id === normalized.id);
		const previous = index >= 0 ? currentActivities[index] : undefined;
		const orderingUnchanged =
			previous !== undefined &&
			isActiveStatus(previous.status) === isActiveStatus(normalized.status) &&
			getActivitySortTime(previous) === getActivitySortTime(normalized);
		let merged = [normalized, ...currentActivities];
		if (index >= 0) {
			merged = [...currentActivities.slice(0, index), normalized, ...currentActivities.slice(index + 1)];
		}
		const activities = (orderingUnchanged ? merged : sortActivities(merged)).slice(0, ACTIVITY_LIST_LIMIT);
		state = {
			...state,
			environmentActivities: {
				...state.environmentActivities,
				[environmentId]: activities
			}
		};
		core.updateEnvironmentState(environmentId, (environmentState) => ({
			...environmentState,
			activities,
			streamError: false,
			errorMessage: undefined
		}));
		let globalIndex = -1;
		if (orderingUnchanged && activities.length === currentActivities.length) {
			globalIndex = _activities.findIndex((item) => item.id === normalized.id);
		}
		if (globalIndex >= 0) {
			setActivities([..._activities.slice(0, globalIndex), normalized, ..._activities.slice(globalIndex + 1)]);
			notifyActivitySubscribers();
		} else {
			rebuildActivities();
		}

		const existingDetail = state.details[normalized.id];
		if (existingDetail) {
			state = {
				...state,
				details: {
					...state.details,
					[normalized.id]: {
						...existingDetail,
						activity: normalized
					}
				}
			};
		}
	}

	function mergeMessage(message: ActivityMessage) {
		const detail = state.details[message.activityId];
		if (!detail) {
			return;
		}

		const exists = detail.messages.some((item) => item.id === message.id);
		const messages = exists ? detail.messages : [...detail.messages, message].slice(-ACTIVITY_DETAIL_LIMIT);
		state = {
			...state,
			details: {
				...state.details,
				[message.activityId]: {
					...detail,
					messages
				}
			}
		};
	}

	function activityEnvironmentId(activityId: string): string {
		const activity = state.details[activityId]?.activity ?? _activities.find((item) => item.id === activityId) ?? null;
		return activity ? sourceEnvironmentId(activity) : requestedEnvironments[activityId] || LOCAL_DOCKER_ENVIRONMENT_ID;
	}

	async function loadDetail(activityId: string) {
		if (state.details[activityId] || state.detailLoadingIds[activityId]) {
			return;
		}
		if (!userStore.hasPermission('activities:read', activityEnvironmentId(activityId))) {
			return;
		}

		const signal = session.signal;
		state = { ...state, detailLoadingIds: { ...state.detailLoadingIds, [activityId]: true } };
		try {
			const detailResult = await tryCatch(
				activityService.getActivity(activityId, activityEnvironmentId(activityId), ACTIVITY_DETAIL_LIMIT)
			);
			if (signal.aborted) {
				return;
			}
			if (detailResult.error !== null) {
				state = { ...state, detailErrors: { ...state.detailErrors, [activityId]: extractApiErrorMessage(detailResult.error) } };
				return;
			}
			const environmentId = activityEnvironmentId(activityId);
			const normalized = {
				...detailResult.data,
				activity: normalizeActivity(detailResult.data.activity, environmentId)
			};
			state = { ...state, details: { ...state.details, [activityId]: normalized } };
			mergeActivity(normalized.activity);
			if (normalized.activity.batchId) setBatchExpanded(normalized.activity.batchId, true);
			state = { ...state, detailErrors: omitKey(state.detailErrors, activityId) };
		} finally {
			if (!signal.aborted) {
				state = { ...state, detailLoadingIds: omitKey(state.detailLoadingIds, activityId) };
			}
		}
	}

	function setActivityExpanded(activityId: string, expanded: boolean) {
		if (!activityId) {
			return;
		}

		if (expanded) {
			if (state.expandedActivityIds[activityId]) {
				return;
			}
			state = { ...state, expandedActivityIds: { ...state.expandedActivityIds, [activityId]: true } };
			void loadDetail(activityId);
		} else {
			if (!state.expandedActivityIds[activityId]) {
				return;
			}
			state = { ...state, expandedActivityIds: omitKey(state.expandedActivityIds, activityId) };
		}
	}

	function toggleActivity(activityId: string) {
		setActivityExpanded(activityId, !state.expandedActivityIds[activityId]);
	}

	function setBatchExpanded(batchId: string, expanded: boolean) {
		if (!batchId) {
			return;
		}
		if (expanded) {
			state = { ...state, expandedBatchIds: { ...state.expandedBatchIds, [batchId]: true } };
		} else {
			state = { ...state, expandedBatchIds: omitKey(state.expandedBatchIds, batchId) };
		}
	}

	function openCenter(activityId?: string, batchId?: string, environmentId?: string) {
		if (activityId && environmentId) requestedEnvironments = { ...requestedEnvironments, [activityId]: environmentId };
		state = { ...state, open: true };
		discardPendingActivityToasts();
		focus(activityId ?? (batchId ? `batch:${batchId}` : null));
		if (activityId) {
			setActivityExpanded(activityId, true);
		}
		if (batchId) {
			setBatchExpanded(batchId, true);
		}
	}

	function focus(key: string | null) {
		state = { ...state, focusedKey: key };
		if (key && !key.startsWith('batch:')) {
			void loadDetail(key);
		}
	}

	function environmentFailures(): ActivityEnvironmentFailure[] {
		return Object.values(core.environmentStates)
			.filter((environmentState) => environmentState.streamError)
			.map((environmentState) => ({
				environmentId: environmentState.id,
				environmentName: environmentState.name,
				message: environmentState.errorMessage
			}));
	}

	const searchTerm = $derived(state.searchTerm);
	const statusFilters = $derived(state.statusFilters);
	const typeFilters = $derived(state.typeFilters);

	function matchesFilters(activity: Activity): boolean {
		if (statusFilters.length > 0 && !statusFilters.includes(activity.status)) {
			return false;
		}
		if (typeFilters.length > 0 && !typeFilters.includes(activity.type)) {
			return false;
		}
		const term = searchTerm.trim().toLowerCase();
		if (term && !activitySearchHaystack(activity).includes(term)) {
			return false;
		}
		return true;
	}

	const allGroups = $derived(groupActivities(_activities));
	const filteredGroups = $derived(groupActivities(_activities.filter(matchesFilters)));
	const runningGroups = $derived(filteredGroups.filter(groupStatusIsActive));
	const historyGroups = $derived(filteredGroups.filter((group) => !groupStatusIsActive(group)));
	const activeCount = $derived(_activities.filter((activity) => isActiveStatus(activity.status)).length);

	return {
		acceptActivity: (activity: Activity) => {
			const current = _activities.find((item) => item.id === activity.id);
			if (current) {
				if (!isActiveStatus(current.status) && isActiveStatus(activity.status)) {
					observedStatusById.set(activity.id, activity.status);
					noteActivityStatus(current);
				}
				return;
			}
			mergeActivity(activity);
		},
		subscribeActivities(subscriber: (activities: readonly Activity[]) => void): () => void {
			activitySubscribers.add(subscriber);
			subscriber(_activities);
			return () => {
				activitySubscribers.delete(subscriber);
			};
		},
		get activities(): Activity[] {
			return _activities;
		},
		get activeUpdateKeys(): ReadonlySet<string> {
			return _activeUpdateKeys;
		},
		get runningGroups(): ActivityGroup[] {
			return runningGroups;
		},
		get historyGroups(): ActivityGroup[] {
			return historyGroups;
		},
		get allGroups(): ActivityGroup[] {
			return allGroups;
		},
		get focusedKey(): string | null {
			return state.focusedKey;
		},
		focus,
		get activeCount(): number {
			return activeCount;
		},
		get searchTerm(): string {
			return searchTerm;
		},
		get statusFilters(): ActivityStatus[] {
			return statusFilters;
		},
		get typeFilters(): ActivityType[] {
			return typeFilters;
		},
		get hasActiveFilters(): boolean {
			return searchTerm.trim() !== '' || statusFilters.length > 0 || typeFilters.length > 0;
		},
		get activeFilterCount(): number {
			return statusFilters.length + typeFilters.length;
		},
		get open(): boolean {
			return state.open;
		},
		get loading(): boolean {
			return Object.values(core.environmentStates).some((environmentState) => environmentState.loading);
		},
		get connected(): boolean {
			return core.streamConnected;
		},
		get streamError(): boolean {
			return core.streamFailed || Object.values(core.environmentStates).some((environmentState) => environmentState.streamError);
		},
		get environmentFailures(): ActivityEnvironmentFailure[] {
			return environmentFailures();
		},
		isExpanded(activityId: string): boolean {
			return !!state.expandedActivityIds[activityId];
		},
		isBatchExpanded(batchId: string): boolean {
			return !!state.expandedBatchIds[batchId];
		},
		setBatchExpanded,
		isDetailLoading(activityId: string): boolean {
			return !!state.detailLoadingIds[activityId];
		},
		detailError(activityId: string): string | undefined {
			return state.detailErrors[activityId];
		},
		isCancelling(activityId: string): boolean {
			return !!state.cancellingIds[activityId];
		},
		getDetail(activityId: string): ActivityDetail | null {
			const activity = state.details[activityId]?.activity ?? _activities.find((item) => item.id === activityId);
			if (!activity) {
				return null;
			}
			return state.details[activityId] ?? { activity, messages: [] };
		},
		getActivity(activityId: string): Activity | null {
			return state.details[activityId]?.activity ?? _activities.find((item) => item.id === activityId) ?? null;
		},
		start: () => core.start(),
		stop: (options?: { resetState?: boolean }) => {
			const wasStarted = core.stop(options);
			if (options?.resetState) {
				session.abort();
				session = new AbortController();
				requestedEnvironments = {};
				observedStatusById.clear();
				completionToastQueuedIds.clear();
				discardPendingActivityToasts();
				setActivities([]);
				state = initialState();
			}
			return wasStarted;
		},
		refresh: () => core.refresh(),
		cancelActivity: async (activityId: string) => {
			if (
				!activityId ||
				state.cancellingIds[activityId] ||
				!userStore.hasPermission('activities:cancel', activityEnvironmentId(activityId))
			) {
				return;
			}
			state = { ...state, cancellingIds: { ...state.cancellingIds, [activityId]: true } };
			try {
				await activityService.cancelActivity(activityId, activityEnvironmentId(activityId));
			} finally {
				state = { ...state, cancellingIds: omitKey(state.cancellingIds, activityId) };
			}
		},
		clearHistory: async (): Promise<ActivityClearHistorySummary> => {
			const signal = session.signal;
			core.reconcileEnvironments();

			const results = await Promise.all(
				Object.values(core.environmentStates)
					.filter((environmentState) => userStore.hasPermission('activities:delete', environmentState.id))
					.map(async (environmentState) => ({
						environmentState,
						cleared: await tryCatch(activityService.clearHistory(environmentState.id))
					}))
			);
			const summary: ActivityClearHistorySummary = {
				deleted: results.reduce((total, { cleared }) => total + (cleared.data?.deleted ?? 0), 0),
				succeeded: results.filter(({ cleared }) => cleared.error === null).length,
				failed: results
					.filter(({ cleared }) => cleared.error !== null)
					.map(({ environmentState, cleared }) => ({
						environmentId: environmentState.id,
						environmentName: environmentState.name,
						message: streamErrorMessage(cleared.error)
					}))
			};

			if (signal.aborted) {
				return summary;
			}

			state = {
				...state,
				details: {},
				expandedActivityIds: {},
				detailLoadingIds: {},
				detailErrors: {}
			};
			await core.refresh();

			return summary;
		},
		setSearchTerm: (term: string) => {
			state = { ...state, searchTerm: term };
		},
		toggleStatusFilter: (status: ActivityStatus) => {
			if (state.statusFilters.includes(status)) {
				state = { ...state, statusFilters: state.statusFilters.filter((item) => item !== status) };
			} else {
				state = { ...state, statusFilters: [...state.statusFilters, status] };
			}
		},
		toggleTypeFilter: (type: ActivityType) => {
			if (state.typeFilters.includes(type)) {
				state = { ...state, typeFilters: state.typeFilters.filter((item) => item !== type) };
			} else {
				state = { ...state, typeFilters: [...state.typeFilters, type] };
			}
		},
		clearFilters: () => {
			state = { ...state, searchTerm: '', statusFilters: [], typeFilters: [] };
		},
		setOpen: (open: boolean) => {
			state = { ...state, open };
			if (open) {
				discardPendingActivityToasts();
			}
		},
		openCenter,
		toggleCenter: () => {
			if (state.open) {
				state = { ...state, open: false };
				return;
			}
			openCenter();
		},
		retryLoadDetail: (activityId: string) => {
			state = {
				...state,
				detailErrors: omitKey(state.detailErrors, activityId),
				details: omitKey(state.details, activityId)
			};
			void loadDetail(activityId);
		},
		retryStream: () => core.retryStream(),
		setActivityExpanded,
		toggleActivity
	};
}

export const activityStore = createActivityStore();
