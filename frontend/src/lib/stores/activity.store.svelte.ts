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
	ActivityBatchGroup,
	ActivityClearHistorySummary,
	ActivityDetail,
	ActivityEnvironmentFailure,
	ActivityGroup,
	ActivityMessage,
	ActivityStatus,
	ActivityStreamEvent,
	ActivityType
} from '#lib/types/activity.type.js';
import type { Environment } from '#lib/types/environment.js';
import { instantEpochMilliseconds, parseInstant } from '#lib/utils/formatting.js';
import { tryCatch } from '#lib/utils/try-catch.js';

const ACTIVITY_LIST_LIMIT = 50;
const ACTIVITY_DETAIL_LIMIT = 500;

type ActivityEnvironmentState = StreamEnvStateBase & {
	activities: Activity[];
};

function sortActivitiesInternal(items: Activity[]): Activity[] {
	const sortTimeMs = new Map(items.map((a) => [a, getActivitySortTimeInternal(a)]));
	return [...items].sort((a, b) => {
		const aActive = isActiveStatusInternal(a.status);
		const bActive = isActiveStatusInternal(b.status);
		if (aActive !== bActive) return aActive ? -1 : 1;
		const diff = (sortTimeMs.get(b) ?? 0) - (sortTimeMs.get(a) ?? 0);
		if (diff !== 0) return diff;
		return b.id.localeCompare(a.id);
	});
}

// Mirrors the backend default order: active rows keep their immutable createdAt
// so progress updates never reshuffle them; terminal rows sort by endedAt.
function getActivitySortTimeInternal(activity: Activity): number {
	const value = activity.endedAt || activity.createdAt || activity.startedAt;
	return instantEpochMilliseconds(value) ?? 0;
}

function isActiveStatusInternal(status: ActivityStatus): boolean {
	return status === 'queued' || status === 'running';
}

function activitySearchHaystackInternal(activity: Activity): string {
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

function groupActivitiesInternal(items: Activity[]): ActivityGroup[] {
	const batchItems = new Map<string, Activity[]>();
	for (const activity of items) {
		if (activity.batchId) batchItems.set(activity.batchId, [...(batchItems.get(activity.batchId) ?? []), activity]);
	}
	return items.flatMap((activity): ActivityGroup[] => {
		const members = activity.batchId ? batchItems.get(activity.batchId) : undefined;
		if (!activity.batchId || !members || members.length === 1) {
			return [{ kind: 'single', activity }];
		}
		if (members[0] !== activity) {
			return [];
		}
		return [batchGroupInternal(activity.batchId, members)];
	});
}

function batchGroupInternal(batchId: string, items: Activity[]): ActivityBatchGroup {
	const jobActivity = items.find((item) => item.type === 'job_run');
	const operations = jobActivity ? items.filter((item) => item.type !== 'job_run') : items;
	const failed = operations.filter((item) => item.status === 'failed').length;
	return {
		kind: 'batch',
		batchId,
		items,
		total: operations.length,
		done: operations.filter((item) => !isActiveStatusInternal(item.status)).length,
		failed,
		status: jobActivity?.status ?? batchStatusInternal(items, failed)
	};
}

function batchStatusInternal(items: Activity[], failed: number): ActivityStatus {
	if (items.some((item) => isActiveStatusInternal(item.status))) {
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

function groupStatusIsActiveInternal(group: ActivityGroup): boolean {
	return group.kind === 'batch' ? isActiveStatusInternal(group.status) : isActiveStatusInternal(group.activity.status);
}

function activeUpdateKeysInternal(activities: readonly Activity[]): Set<string> {
	return new Set(
		activities
			.filter(
				(activity) =>
					activity.type === 'auto_update' && activity.resourceType === 'container' && isActiveStatusInternal(activity.status)
			)
			.map((activity) => `${activity.sourceEnvironmentId || activity.environmentId}:${activity.resourceId}`)
	);
}

function omitKeyInternal<T>(record: Record<string, T>, key: string): Record<string, T> {
	return Object.fromEntries(Object.entries(record).filter(([entryKey]) => entryKey !== key));
}

type ActivityStoreState = {
	environmentActivities: Record<string, Activity[]>;
	details: Record<string, ActivityDetail>;
	expandedActivityIds: Record<string, boolean>;
	detailLoadingIds: Record<string, boolean>;
	detailErrorIds: Record<string, boolean>;
	cancellingIds: Record<string, boolean>;
	expandedBatchIds: Record<string, boolean>;
	searchTerm: string;
	statusFilters: ActivityStatus[];
	typeFilters: ActivityType[];
	open: boolean;
	focusedKey: string | null;
};

function initialStateInternal(): ActivityStoreState {
	return {
		environmentActivities: {},
		details: {},
		expandedActivityIds: {},
		detailLoadingIds: {},
		detailErrorIds: {},
		cancellingIds: {},
		expandedBatchIds: {},
		searchTerm: '',
		statusFilters: [],
		typeFilters: [],
		open: false,
		focusedKey: null
	};
}

function sourceEnvironmentIdInternal(activity: Activity | null | undefined): string {
	return activity?.sourceEnvironmentId || activity?.environmentId || LOCAL_DOCKER_ENVIRONMENT_ID;
}

function createActivityStore() {
	let state = $state.raw<ActivityStoreState>(initialStateInternal());
	let _activities = $state.raw<Activity[]>([]);
	let _activeUpdateKeys = $state.raw<ReadonlySet<string>>(new Set());
	let session = new AbortController();
	let requestedEnvironments: Record<string, string> = {};
	const observedStatusById = new Map<string, ActivityStatus>();
	const completionToastQueuedIds = new Set<string>();
	const activitySubscribers = new Set<(activities: readonly Activity[]) => void>();

	function setActivitiesInternal(activities: Activity[]) {
		_activities = activities;
		const keys = activeUpdateKeysInternal(activities);
		if (keys.size !== _activeUpdateKeys.size || [...keys].some((key) => !_activeUpdateKeys.has(key))) {
			_activeUpdateKeys = keys;
		}
	}

	function notifyActivitySubscribersInternal() {
		for (const subscriber of activitySubscribers) subscriber(_activities);
	}

	function noteActivityStatusInternal(activity: Activity) {
		const prev = observedStatusById.get(activity.id);
		observedStatusById.set(activity.id, activity.status);
		if (!prev || !isActiveStatusInternal(prev) || prev === activity.status || completionToastQueuedIds.has(activity.id)) {
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
		queueActivityCompletionToast(activity, openCenterInternal);
	}

	const core = createEnvironmentStreamStore<ActivityEnvironmentState, ActivityStreamEvent>({
		label: 'Activity',
		includeEnvironment: (environment) => userStore.hasPermission('activities:read', environment.id),
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
					replaceEnvironmentSnapshotInternal(environmentId, event.activities ?? []);
					break;
				case 'activity':
					if (event.activity) {
						mergeActivityInternal(normalizeActivityInternal(event.activity, environmentId));
					}
					break;
				case 'message':
					if (event.message) {
						mergeMessageInternal(event.message);
					}
					break;
				case 'error':
					core.setEnvironmentError(environmentId, new Error(event.error || 'Activity stream error'));
					break;
			}
		},
		async fetchSnapshot(environmentId, signal) {
			core.updateEnvironmentState(environmentId, (environmentState) => ({
				...environmentState,
				loading: true
			}));

			const operationResult = await tryCatch(
				(async () => {
					const result = await activityService.getActivities(
						{ pagination: { page: 1, limit: ACTIVITY_LIST_LIMIT } },
						environmentId
					);
					if (signal.aborted || !core.environmentState(environmentId)) {
						return;
					}
					replaceEnvironmentSnapshotInternal(environmentId, result.data ?? []);
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;
				if (!signal.aborted && core.environmentState(environmentId)) {
					console.warn('Failed to refresh activities:', error);
					core.setEnvironmentError(environmentId, error);
				}
			} else {
				return operationResult.data;
			}
		},
		onEnvironmentRemoved(environmentId) {
			state = {
				...state,
				environmentActivities: omitKeyInternal(state.environmentActivities, environmentId)
			};
			rebuildActivitiesInternal();
		},
		onEnvironmentRenamed(environmentId, name) {
			const existing = core.environmentState(environmentId);
			const updatedActivities = (state.environmentActivities[environmentId] ?? existing?.activities ?? []).map((activity) =>
				activity.sourceEnvironmentName
					? activity
					: {
							...activity,
							sourceEnvironmentName: name
						}
			);
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
			rebuildActivitiesInternal();
		}
	});

	function normalizeActivityInternal(activity: Activity, environmentId: string): Activity {
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

	function replaceEnvironmentSnapshotInternal(environmentId: string, activities: Activity[]) {
		if (!core.environmentState(environmentId)) {
			return;
		}
		const normalizedActivities = sortActivitiesInternal(
			activities.map((activity) => normalizeActivityInternal(activity, environmentId))
		);
		let details = state.details;
		for (const activity of normalizedActivities) {
			noteActivityStatusInternal(activity);
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
		rebuildActivitiesInternal();
	}

	function rebuildActivitiesInternal() {
		const activities = sortActivitiesInternal(Object.values(state.environmentActivities).flat());
		setActivitiesInternal(activities);

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
		notifyActivitySubscribersInternal();
	}

	function mergeActivityInternal(activity: Activity) {
		const environmentId = sourceEnvironmentIdInternal(activity);
		const normalized = normalizeActivityInternal(activity, environmentId);
		noteActivityStatusInternal(normalized);
		const currentActivities =
			state.environmentActivities[environmentId] ?? core.environmentState(environmentId)?.activities ?? [];
		const index = currentActivities.findIndex((item) => item.id === normalized.id);
		const previous = index >= 0 ? currentActivities[index] : undefined;
		const orderingUnchanged =
			previous !== undefined &&
			isActiveStatusInternal(previous.status) === isActiveStatusInternal(normalized.status) &&
			getActivitySortTimeInternal(previous) === getActivitySortTimeInternal(normalized);
		const merged =
			index >= 0
				? [...currentActivities.slice(0, index), normalized, ...currentActivities.slice(index + 1)]
				: [normalized, ...currentActivities];
		const activities = (orderingUnchanged ? merged : sortActivitiesInternal(merged)).slice(0, ACTIVITY_LIST_LIMIT);
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
		const globalIndex =
			orderingUnchanged && activities.length === currentActivities.length
				? _activities.findIndex((item) => item.id === normalized.id)
				: -1;
		if (globalIndex >= 0) {
			setActivitiesInternal([..._activities.slice(0, globalIndex), normalized, ..._activities.slice(globalIndex + 1)]);
			notifyActivitySubscribersInternal();
		} else {
			rebuildActivitiesInternal();
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

	function mergeMessageInternal(message: ActivityMessage) {
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

	function activityEnvironmentIdInternal(activityId: string): string {
		const activity = state.details[activityId]?.activity ?? _activities.find((item) => item.id === activityId) ?? null;
		return activity ? sourceEnvironmentIdInternal(activity) : requestedEnvironments[activityId] || LOCAL_DOCKER_ENVIRONMENT_ID;
	}

	async function loadDetailInternal(activityId: string) {
		if (state.details[activityId] || state.detailLoadingIds[activityId]) {
			return;
		}

		const signal = session.signal;
		state = { ...state, detailLoadingIds: { ...state.detailLoadingIds, [activityId]: true } };
		try {
			const operationResult = await tryCatch(
				(async () => {
					const detail = await activityService.getActivity(
						activityId,
						activityEnvironmentIdInternal(activityId),
						ACTIVITY_DETAIL_LIMIT
					);
					if (signal.aborted) {
						return;
					}
					const environmentId = activityEnvironmentIdInternal(activityId);
					const normalized = {
						...detail,
						activity: normalizeActivityInternal(detail.activity, environmentId)
					};
					state = { ...state, details: { ...state.details, [activityId]: normalized } };
					mergeActivityInternal(normalized.activity);
					if (normalized.activity.batchId) setBatchExpandedInternal(normalized.activity.batchId, true);
					state = { ...state, detailErrorIds: omitKeyInternal(state.detailErrorIds, activityId) };
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;
				if (signal.aborted) {
					return;
				}
				console.warn('Failed to load activity detail:', error);
				state = { ...state, detailErrorIds: { ...state.detailErrorIds, [activityId]: true } };
			} else {
				return operationResult.data;
			}
		} finally {
			if (!signal.aborted) {
				state = { ...state, detailLoadingIds: omitKeyInternal(state.detailLoadingIds, activityId) };
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
			void loadDetailInternal(activityId);
		} else {
			if (!state.expandedActivityIds[activityId]) {
				return;
			}
			state = { ...state, expandedActivityIds: omitKeyInternal(state.expandedActivityIds, activityId) };
		}
	}

	function toggleActivity(activityId: string) {
		setActivityExpanded(activityId, !state.expandedActivityIds[activityId]);
	}

	function setBatchExpandedInternal(batchId: string, expanded: boolean) {
		if (!batchId) {
			return;
		}
		state = {
			...state,
			expandedBatchIds: expanded
				? { ...state.expandedBatchIds, [batchId]: true }
				: omitKeyInternal(state.expandedBatchIds, batchId)
		};
	}

	function openCenterInternal(activityId?: string, batchId?: string, environmentId?: string) {
		if (activityId && environmentId) requestedEnvironments = { ...requestedEnvironments, [activityId]: environmentId };
		state = { ...state, open: true };
		discardPendingActivityToasts();
		focusInternal(activityId ?? (batchId ? `batch:${batchId}` : null));
		if (activityId) {
			setActivityExpanded(activityId, true);
		}
		if (batchId) {
			setBatchExpandedInternal(batchId, true);
		}
	}

	function focusInternal(key: string | null) {
		state = { ...state, focusedKey: key };
		if (key && !key.startsWith('batch:')) {
			void loadDetailInternal(key);
		}
	}

	function environmentFailuresInternal(): ActivityEnvironmentFailure[] {
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

	function matchesFiltersInternal(activity: Activity): boolean {
		if (statusFilters.length > 0 && !statusFilters.includes(activity.status)) {
			return false;
		}
		if (typeFilters.length > 0 && !typeFilters.includes(activity.type)) {
			return false;
		}
		const term = searchTerm.trim().toLowerCase();
		if (term && !activitySearchHaystackInternal(activity).includes(term)) {
			return false;
		}
		return true;
	}

	const allGroups = $derived(groupActivitiesInternal(_activities));
	const filteredGroups = $derived(groupActivitiesInternal(_activities.filter(matchesFiltersInternal)));
	const runningGroups = $derived(filteredGroups.filter(groupStatusIsActiveInternal));
	const historyGroups = $derived(filteredGroups.filter((group) => !groupStatusIsActiveInternal(group)));
	const activeCount = $derived(_activities.filter((activity) => isActiveStatusInternal(activity.status)).length);

	return {
		acceptActivity: (activity: Activity) => {
			const current = _activities.find((item) => item.id === activity.id);
			if (current) {
				if (!isActiveStatusInternal(current.status) && isActiveStatusInternal(activity.status)) {
					observedStatusById.set(activity.id, activity.status);
					noteActivityStatusInternal(current);
				}
				return;
			}
			mergeActivityInternal(activity);
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
		focus: focusInternal,
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
			return environmentFailuresInternal();
		},
		isExpanded(activityId: string): boolean {
			return !!state.expandedActivityIds[activityId];
		},
		isBatchExpanded(batchId: string): boolean {
			return !!state.expandedBatchIds[batchId];
		},
		setBatchExpanded: setBatchExpandedInternal,
		isDetailLoading(activityId: string): boolean {
			return !!state.detailLoadingIds[activityId];
		},
		isDetailError(activityId: string): boolean {
			return !!state.detailErrorIds[activityId];
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
				setActivitiesInternal([]);
				state = initialStateInternal();
			}
			return wasStarted;
		},
		refresh: () => core.refresh(),
		cancelActivity: async (activityId: string) => {
			if (!activityId || state.cancellingIds[activityId]) {
				return;
			}
			state = { ...state, cancellingIds: { ...state.cancellingIds, [activityId]: true } };
			try {
				await activityService.cancelActivity(activityId, activityEnvironmentIdInternal(activityId));
			} finally {
				state = { ...state, cancellingIds: omitKeyInternal(state.cancellingIds, activityId) };
			}
		},
		clearHistory: async (): Promise<ActivityClearHistorySummary> => {
			const signal = session.signal;
			core.reconcileEnvironments();

			const results = await Promise.all(
				Object.values(core.environmentStates).map(async (environmentState) => ({
					environmentState,
					operationResult: await tryCatch(activityService.clearHistory(environmentState.id))
				}))
			);
			const summary: ActivityClearHistorySummary = {
				deleted: results.reduce((total, { operationResult }) => total + (operationResult.data?.deleted ?? 0), 0),
				succeeded: results.filter(({ operationResult }) => operationResult.error === null).length,
				failed: results
					.filter(({ operationResult }) => operationResult.error !== null)
					.map(({ environmentState, operationResult }) => ({
						environmentId: environmentState.id,
						environmentName: environmentState.name,
						message: streamErrorMessage(operationResult.error)
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
				detailErrorIds: {}
			};
			await core.refresh();

			return summary;
		},
		setSearchTerm: (term: string) => {
			state = { ...state, searchTerm: term };
		},
		toggleStatusFilter: (status: ActivityStatus) => {
			state = {
				...state,
				statusFilters: state.statusFilters.includes(status)
					? state.statusFilters.filter((item) => item !== status)
					: [...state.statusFilters, status]
			};
		},
		toggleTypeFilter: (type: ActivityType) => {
			state = {
				...state,
				typeFilters: state.typeFilters.includes(type)
					? state.typeFilters.filter((item) => item !== type)
					: [...state.typeFilters, type]
			};
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
		openCenter: openCenterInternal,
		toggleCenter: () => {
			if (state.open) {
				state = { ...state, open: false };
				return;
			}
			openCenterInternal();
		},
		retryLoadDetail: (activityId: string) => {
			state = {
				...state,
				detailErrorIds: omitKeyInternal(state.detailErrorIds, activityId),
				details: omitKeyInternal(state.details, activityId)
			};
			void loadDetailInternal(activityId);
		},
		retryStream: () => core.retryStream(),
		setActivityExpanded,
		toggleActivity
	};
}

export const activityStore = createActivityStore();
