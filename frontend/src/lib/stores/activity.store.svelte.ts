import { activityService } from '$lib/services/activity-service';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
import {
	createEnvironmentStreamStore,
	environmentDisplayName,
	streamErrorMessage,
	type StreamEnvStateBase
} from '$lib/stores/environment-stream.svelte';
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
} from '$lib/types/activity.type';
import type { Environment } from '$lib/types/environment';
import userStore from '$lib/stores/user-store';
import { get } from 'svelte/store';
import { discardPendingActivityToasts, queueActivityCompletionToast } from '$lib/components/activity/activity-completion-toasts';

const ACTIVITY_LIST_LIMIT = 50;
const ACTIVITY_DETAIL_LIMIT = 500;

type ActivityEnvironmentState = StreamEnvStateBase & {
	activities: Activity[];
};

function sortActivitiesInternal(items: Activity[]): Activity[] {
	return [...items].sort((a, b) => {
		const aActive = isActiveStatusInternal(a.status);
		const bActive = isActiveStatusInternal(b.status);
		if (aActive !== bActive) return aActive ? -1 : 1;
		const diff = getActivitySortTimeInternal(b) - getActivitySortTimeInternal(a);
		if (diff !== 0) return diff;
		return b.id.localeCompare(a.id);
	});
}

// Mirrors the backend default order: active rows keep their immutable createdAt
// so progress updates never reshuffle them; terminal rows sort by endedAt.
function getActivitySortTimeInternal(activity: Activity): number {
	const value = activity.endedAt || activity.createdAt || activity.startedAt;
	return value ? new Date(value).getTime() : 0;
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
		activity.step,
		activity.error,
		activity.sourceEnvironmentName
	]
		.filter(Boolean)
		.join('\n')
		.toLowerCase();
}

// Groups consecutive-by-batch activities of an already-sorted list into
// ActivityGroups. A batch group sits at the position of its first (newest)
// member, so group positions inherit the stable activity ordering.
function groupActivitiesInternal(items: Activity[]): ActivityGroup[] {
	const groups: ActivityGroup[] = [];
	const batches = new Map<string, ActivityBatchGroup>();
	for (const activity of items) {
		const batchId = activity.batchId;
		if (!batchId) {
			groups.push({ kind: 'single', activity });
			continue;
		}
		const existing = batches.get(batchId);
		if (existing) {
			existing.items.push(activity);
			continue;
		}
		const group: ActivityBatchGroup = {
			kind: 'batch',
			batchId,
			items: [activity],
			total: 0,
			done: 0,
			failed: 0,
			status: 'running',
			progress: null
		};
		batches.set(batchId, group);
		groups.push(group);
	}

	for (const group of batches.values()) {
		// A "batch" of one renders as a plain activity row.
		const only = group.items.length === 1 ? group.items[0] : undefined;
		if (only) {
			const index = groups.indexOf(group);
			groups[index] = { kind: 'single', activity: only };
			continue;
		}
		finalizeBatchGroupInternal(group);
	}
	return groups;
}

function finalizeBatchGroupInternal(group: ActivityBatchGroup) {
	group.total = group.items.length;
	group.done = group.items.filter((item) => !isActiveStatusInternal(item.status)).length;
	group.failed = group.items.filter((item) => item.status === 'failed').length;
	if (group.items.some((item) => isActiveStatusInternal(item.status))) {
		group.status = group.items.some((item) => item.status === 'running') ? 'running' : 'queued';
		const memberProgress = group.items.map((item) =>
			isActiveStatusInternal(item.status) ? clampBatchProgressInternal(item.progress) : 100
		);
		group.progress = Math.round(memberProgress.reduce((sum, value) => sum + value, 0) / group.total);
	} else {
		if (group.failed > 0) {
			group.status = 'failed';
		} else if (group.items.every((item) => item.status === 'cancelled')) {
			group.status = 'cancelled';
		} else {
			group.status = 'success';
		}
		group.progress = 100;
	}
}

function clampBatchProgressInternal(progress: number | null | undefined): number {
	if (typeof progress !== 'number' || Number.isNaN(progress)) {
		return 0;
	}
	return Math.min(100, Math.max(0, progress));
}

function groupStatusIsActiveInternal(group: ActivityGroup): boolean {
	return group.kind === 'batch' ? isActiveStatusInternal(group.status) : isActiveStatusInternal(group.activity.status);
}

function sourceEnvironmentIdInternal(activity: Activity | null | undefined): string {
	return activity?.sourceEnvironmentId || activity?.environmentId || LOCAL_DOCKER_ENVIRONMENT_ID;
}

function createActivityStore() {
	let _activities = $state<Activity[]>([]);
	let _environmentActivities = $state<Record<string, Activity[]>>({});
	let _details = $state<Record<string, ActivityDetail>>({});
	let _expandedActivityIds = $state<Record<string, boolean>>({});
	let _detailLoadingIds = $state<Record<string, boolean>>({});
	let _detailErrorIds = $state<Record<string, boolean>>({});
	let _cancellingIds = $state<Record<string, boolean>>({});
	let _expandedBatchIds = $state<Record<string, boolean>>({});
	let _searchTerm = $state('');
	let _statusFilters = $state<ActivityStatus[]>([]);
	let _typeFilters = $state<ActivityType[]>([]);
	let _open = $state(false);
	let _currentEnvironmentId = $state(LOCAL_DOCKER_ENVIRONMENT_ID);
	let sessionGeneration = 0;
	// Last observed status per activity, for completion-toast transition
	// detection. Intentionally non-reactive: only stream handling reads it.
	const observedStatusById = new Map<string, ActivityStatus>();

	// Toast when an activity this session observed as active reaches
	// success/failed while the sheet is closed. Activities that first appear
	// already terminal (history snapshots on load) and other users' work
	// (schedulers, other admins) stay silent.
	function noteActivityStatusInternal(activity: Activity) {
		const prev = observedStatusById.get(activity.id);
		observedStatusById.set(activity.id, activity.status);
		if (!prev || !isActiveStatusInternal(prev) || prev === activity.status) {
			return;
		}
		if (activity.status !== 'success' && activity.status !== 'failed') {
			return;
		}
		if (_open) {
			return;
		}
		// Only the initiating user's own actions toast — scheduled jobs and
		// other users' work carry no/another userId and stay silent.
		const currentUserId = get(userStore)?.id;
		if (!activity.startedBy?.userId || !currentUserId || activity.startedBy.userId !== currentUserId) {
			return;
		}
		queueActivityCompletionToast(activity);
	}

	const core = createEnvironmentStreamStore<ActivityEnvironmentState, ActivityStreamEvent>({
		label: 'Activity',
		includeEnvironment: (environment) => userStore.hasPermission('activities:read', environment.id),
		subscribeEnvironmentFilter: (reconcile) => userStore.subscribe(reconcile),
		createEnvironmentState(environment: Pick<Environment, 'id' | 'name'>): ActivityEnvironmentState {
			return {
				id: environment.id || LOCAL_DOCKER_ENVIRONMENT_ID,
				name: environmentDisplayName(environment),
				activities: [],
				loading: true,
				streamError: false
			};
		},
		openStream: (signal) => activityService.openActivityStream(signal, ACTIVITY_LIST_LIMIT),
		// REST-fetch history on start so the panel isn't stuck loading if the
		// stream is slow or drops before delivering its first snapshot.
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
		async fetchSnapshot(environmentId, generation) {
			core.updateEnvironmentState(environmentId, (state) => ({
				...state,
				loading: true
			}));
			try {
				const result = await activityService.getActivities(
					{ pagination: { page: 1, limit: ACTIVITY_LIST_LIMIT } },
					environmentId
				);
				// The environment can be removed while the fetch is in-flight; don't resurrect it.
				if (!core.isCurrentGeneration(generation) || !core.environmentState(environmentId)) {
					return;
				}
				replaceEnvironmentSnapshotInternal(environmentId, result.data ?? []);
			} catch (error) {
				if (core.isCurrentGeneration(generation) && core.environmentState(environmentId)) {
					console.warn('Failed to refresh activities:', error);
					core.setEnvironmentError(environmentId, error);
				}
			}
		},
		onEnvironmentRemoved(environmentId) {
			const nextActivities = { ..._environmentActivities };
			delete nextActivities[environmentId];
			_environmentActivities = nextActivities;
			rebuildActivitiesInternal();
		},
		onEnvironmentRenamed(environmentId, name) {
			const existing = core.environmentState(environmentId);
			const updatedActivities = (_environmentActivities[environmentId] ?? existing?.activities ?? []).map((activity) =>
				activity.sourceEnvironmentName
					? activity
					: {
							...activity,
							sourceEnvironmentName: name
						}
			);
			_environmentActivities = {
				..._environmentActivities,
				[environmentId]: updatedActivities
			};
			core.updateEnvironmentState(environmentId, (state) => ({
				...state,
				name,
				activities: updatedActivities
			}));
			rebuildActivitiesInternal();
		},
		onSelectedEnvironment(environment) {
			_currentEnvironmentId = environment?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID;
		}
	});

	function normalizeActivityInternal(activity: Activity, environmentId: string): Activity {
		const state = core.environmentState(environmentId);
		return {
			...activity,
			sourceEnvironmentId: activity.sourceEnvironmentId || environmentId,
			sourceEnvironmentName: activity.sourceEnvironmentName || state?.name || environmentId
		};
	}

	function replaceEnvironmentSnapshotInternal(environmentId: string, activities: Activity[]) {
		// Snapshots can still arrive (stream or in-flight REST) after the
		// environment was removed locally; don't resurrect it.
		if (!core.environmentState(environmentId)) {
			return;
		}
		const normalizedActivities = sortActivitiesInternal(
			activities.map((activity) => normalizeActivityInternal(activity, environmentId))
		);
		// Remote environments only deliver snapshots, so transition detection
		// for completion toasts has to happen here as well as in merges.
		for (const activity of normalizedActivities) {
			noteActivityStatusInternal(activity);
		}
		_environmentActivities = {
			..._environmentActivities,
			[environmentId]: normalizedActivities
		};
		core.updateEnvironmentState(environmentId, (state) => ({
			...state,
			activities: normalizedActivities,
			loading: false,
			streamError: false,
			errorMessage: undefined
		}));
		rebuildActivitiesInternal();
	}

	function rebuildActivitiesInternal() {
		_activities = sortActivitiesInternal(Object.values(_environmentActivities).flat());

		const present = new Set(_activities.map((activity) => activity.id));
		const nextExpanded: Record<string, boolean> = {};
		for (const id of Object.keys(_expandedActivityIds)) {
			if (_expandedActivityIds[id] && present.has(id)) {
				nextExpanded[id] = true;
			}
		}
		_expandedActivityIds = nextExpanded;

		for (const id of observedStatusById.keys()) {
			if (!present.has(id)) {
				observedStatusById.delete(id);
			}
		}
	}

	function mergeActivityInternal(activity: Activity) {
		const environmentId = sourceEnvironmentIdInternal(activity);
		const normalized = normalizeActivityInternal(activity, environmentId);
		noteActivityStatusInternal(normalized);
		const currentActivities = _environmentActivities[environmentId] ?? core.environmentState(environmentId)?.activities ?? [];
		const index = currentActivities.findIndex((item) => item.id === normalized.id);
		const activities = sortActivitiesInternal(
			index >= 0
				? [...currentActivities.slice(0, index), normalized, ...currentActivities.slice(index + 1)]
				: [normalized, ...currentActivities]
		).slice(0, ACTIVITY_LIST_LIMIT);
		_environmentActivities = {
			..._environmentActivities,
			[environmentId]: activities
		};
		core.updateEnvironmentState(environmentId, (state) => {
			return {
				...state,
				activities,
				streamError: false,
				errorMessage: undefined
			};
		});
		rebuildActivitiesInternal();

		const existingDetail = _details[normalized.id];
		if (existingDetail) {
			_details = {
				..._details,
				[normalized.id]: {
					...existingDetail,
					activity: normalized
				}
			};
		}
	}

	function mergeMessageInternal(message: ActivityMessage) {
		const detail = _details[message.activityId];
		if (!detail) {
			return;
		}

		const exists = detail.messages.some((item) => item.id === message.id);
		const messages = exists ? detail.messages : [...detail.messages, message].slice(-ACTIVITY_DETAIL_LIMIT);
		_details = {
			..._details,
			[message.activityId]: {
				...detail,
				messages
			}
		};
	}

	function activityEnvironmentIdInternal(activityId: string): string {
		const activity = _details[activityId]?.activity ?? _activities.find((item) => item.id === activityId) ?? null;
		return sourceEnvironmentIdInternal(activity) || _currentEnvironmentId || LOCAL_DOCKER_ENVIRONMENT_ID;
	}

	async function loadDetailInternal(activityId: string) {
		if (_details[activityId] || _detailLoadingIds[activityId]) {
			return;
		}

		const generation = sessionGeneration;
		_detailLoadingIds = { ..._detailLoadingIds, [activityId]: true };
		try {
			const detail = await activityService.getActivity(
				activityId,
				activityEnvironmentIdInternal(activityId),
				ACTIVITY_DETAIL_LIMIT
			);
			if (generation !== sessionGeneration) {
				return;
			}
			const environmentId = sourceEnvironmentIdInternal(detail.activity);
			const normalized = {
				...detail,
				activity: normalizeActivityInternal(detail.activity, environmentId)
			};
			_details = { ..._details, [activityId]: normalized };
			const nextErrors = { ..._detailErrorIds };
			delete nextErrors[activityId];
			_detailErrorIds = nextErrors;
		} catch (error) {
			if (generation !== sessionGeneration) {
				return;
			}
			console.warn('Failed to load activity detail:', error);
			_detailErrorIds = { ..._detailErrorIds, [activityId]: true };
		} finally {
			if (generation === sessionGeneration) {
				const next = { ..._detailLoadingIds };
				delete next[activityId];
				_detailLoadingIds = next;
			}
		}
	}

	function setActivityExpanded(activityId: string, expanded: boolean) {
		if (!activityId) {
			return;
		}

		if (expanded) {
			if (_expandedActivityIds[activityId]) {
				return;
			}
			_expandedActivityIds = { ..._expandedActivityIds, [activityId]: true };
			void loadDetailInternal(activityId);
		} else {
			if (!_expandedActivityIds[activityId]) {
				return;
			}
			const next = { ..._expandedActivityIds };
			delete next[activityId];
			_expandedActivityIds = next;
		}
	}

	function toggleActivity(activityId: string) {
		setActivityExpanded(activityId, !_expandedActivityIds[activityId]);
	}

	function setBatchExpandedInternal(batchId: string, expanded: boolean) {
		if (!batchId) {
			return;
		}
		if (expanded) {
			_expandedBatchIds = { ..._expandedBatchIds, [batchId]: true };
		} else {
			const next = { ..._expandedBatchIds };
			delete next[batchId];
			_expandedBatchIds = next;
		}
	}

	function environmentFailuresInternal(): ActivityEnvironmentFailure[] {
		return Object.values(core.environmentStates)
			.filter((state) => state.streamError)
			.map((state) => ({
				environmentId: state.id,
				environmentName: state.name,
				message: state.errorMessage
			}));
	}

	function matchesFiltersInternal(activity: Activity): boolean {
		if (_statusFilters.length > 0 && !_statusFilters.includes(activity.status)) {
			return false;
		}
		if (_typeFilters.length > 0 && !_typeFilters.includes(activity.type)) {
			return false;
		}
		const term = _searchTerm.trim().toLowerCase();
		if (term && !activitySearchHaystackInternal(activity).includes(term)) {
			return false;
		}
		return true;
	}

	function filteredGroupsInternal(): ActivityGroup[] {
		return groupActivitiesInternal(_activities.filter(matchesFiltersInternal));
	}

	return {
		get activities(): Activity[] {
			return _activities;
		},
		// A batch containing any active member renders once, in the running
		// section, so completing members never duplicate into history early.
		get runningGroups(): ActivityGroup[] {
			return filteredGroupsInternal().filter(groupStatusIsActiveInternal);
		},
		get historyGroups(): ActivityGroup[] {
			return filteredGroupsInternal().filter((group) => !groupStatusIsActiveInternal(group));
		},
		get activeCount(): number {
			return _activities.filter((activity) => isActiveStatusInternal(activity.status)).length;
		},
		get searchTerm(): string {
			return _searchTerm;
		},
		get statusFilters(): ActivityStatus[] {
			return _statusFilters;
		},
		get typeFilters(): ActivityType[] {
			return _typeFilters;
		},
		get hasActiveFilters(): boolean {
			return _searchTerm.trim() !== '' || _statusFilters.length > 0 || _typeFilters.length > 0;
		},
		get activeFilterCount(): number {
			return _statusFilters.length + _typeFilters.length;
		},
		get open(): boolean {
			return _open;
		},
		get loading(): boolean {
			return Object.values(core.environmentStates).some((state) => state.loading);
		},
		get connected(): boolean {
			return core.streamConnected;
		},
		get streamError(): boolean {
			return core.streamFailed || Object.values(core.environmentStates).some((state) => state.streamError);
		},
		get environmentFailures(): ActivityEnvironmentFailure[] {
			return environmentFailuresInternal();
		},
		get currentEnvironmentId(): string {
			return _currentEnvironmentId;
		},
		isExpanded(activityId: string): boolean {
			return !!_expandedActivityIds[activityId];
		},
		isBatchExpanded(batchId: string): boolean {
			return !!_expandedBatchIds[batchId];
		},
		setBatchExpanded: setBatchExpandedInternal,
		isDetailLoading(activityId: string): boolean {
			return !!_detailLoadingIds[activityId];
		},
		isDetailError(activityId: string): boolean {
			return !!_detailErrorIds[activityId];
		},
		isCancelling(activityId: string): boolean {
			return !!_cancellingIds[activityId];
		},
		getDetail(activityId: string): ActivityDetail | null {
			const activity = _details[activityId]?.activity ?? _activities.find((item) => item.id === activityId);
			if (!activity) {
				return null;
			}
			return _details[activityId] ?? { activity, messages: [] };
		},
		getActivity(activityId: string): Activity | null {
			return _details[activityId]?.activity ?? _activities.find((item) => item.id === activityId) ?? null;
		},
		start: () => core.start(),
		stop: (options?: { resetState?: boolean }) => {
			const wasStarted = core.stop(options);
			if (options?.resetState) {
				sessionGeneration += 1;
				observedStatusById.clear();
				discardPendingActivityToasts();
				_activities = [];
				_environmentActivities = {};
				_details = {};
				_expandedActivityIds = {};
				_expandedBatchIds = {};
				_detailLoadingIds = {};
				_detailErrorIds = {};
				_cancellingIds = {};
				_searchTerm = '';
				_statusFilters = [];
				_typeFilters = [];
				_open = false;
				_currentEnvironmentId = LOCAL_DOCKER_ENVIRONMENT_ID;
			}
			return wasStarted;
		},
		refresh: () => core.refresh(),
		cancelActivity: async (activityId: string) => {
			if (!activityId || _cancellingIds[activityId]) {
				return;
			}
			_cancellingIds = { ..._cancellingIds, [activityId]: true };
			try {
				// The cancelled status arrives via the stream (mergeActivityInternal);
				// callers handle success/error toasts.
				await activityService.cancelActivity(activityId, activityEnvironmentIdInternal(activityId));
			} finally {
				const next = { ..._cancellingIds };
				delete next[activityId];
				_cancellingIds = next;
			}
		},
		clearHistory: async (): Promise<ActivityClearHistorySummary> => {
			const generation = sessionGeneration;
			core.reconcileEnvironments();

			let deleted = 0;
			let succeeded = 0;
			const failed: ActivityEnvironmentFailure[] = [];
			const states = Object.values(core.environmentStates);
			await Promise.all(
				states.map(async (state) => {
					try {
						const result = await activityService.clearHistory(state.id);
						deleted += result.deleted ?? 0;
						succeeded += 1;
					} catch (error) {
						failed.push({
							environmentId: state.id,
							environmentName: state.name,
							message: streamErrorMessage(error)
						});
					}
				})
			);

			if (generation !== sessionGeneration) {
				return { deleted, succeeded, failed };
			}

			_details = {};
			_expandedActivityIds = {};
			_detailLoadingIds = {};
			_detailErrorIds = {};
			await core.refresh();

			return {
				deleted,
				succeeded,
				failed
			};
		},
		setSearchTerm: (term: string) => {
			_searchTerm = term;
		},
		toggleStatusFilter: (status: ActivityStatus) => {
			_statusFilters = _statusFilters.includes(status)
				? _statusFilters.filter((item) => item !== status)
				: [..._statusFilters, status];
		},
		toggleTypeFilter: (type: ActivityType) => {
			_typeFilters = _typeFilters.includes(type) ? _typeFilters.filter((item) => item !== type) : [..._typeFilters, type];
		},
		clearFilters: () => {
			_searchTerm = '';
			_statusFilters = [];
			_typeFilters = [];
		},
		setOpen: (open: boolean) => {
			_open = open;
			if (open) {
				discardPendingActivityToasts();
			}
		},
		openCenter: (activityId?: string, batchId?: string) => {
			_open = true;
			discardPendingActivityToasts();
			if (activityId) {
				setActivityExpanded(activityId, true);
			}
			if (batchId) {
				setBatchExpandedInternal(batchId, true);
			}
		},
		retryLoadDetail: (activityId: string) => {
			const nextErrors = { ..._detailErrorIds };
			delete nextErrors[activityId];
			_detailErrorIds = nextErrors;
			const nextDetails = { ..._details };
			delete nextDetails[activityId];
			_details = nextDetails;
			void loadDetailInternal(activityId);
		},
		retryStream: () => core.retryStream(),
		setActivityExpanded,
		toggleActivity
	};
}

export const activityStore = createActivityStore();
