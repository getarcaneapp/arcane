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
	ActivityClearHistorySummary,
	ActivityDetail,
	ActivityEnvironmentFailure,
	ActivityFilter,
	ActivityMessage,
	ActivityStatus,
	ActivityStreamEvent
} from '$lib/types/activity.type';
import type { Environment } from '$lib/types/environment';
import userStore from '$lib/stores/user-store';

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
		return getActivitySortTimeInternal(b) - getActivitySortTimeInternal(a);
	});
}

function getActivitySortTimeInternal(activity: Activity): number {
	const value = activity.updatedAt || activity.endedAt || activity.startedAt || activity.createdAt;
	return value ? new Date(value).getTime() : 0;
}

function isActiveStatusInternal(status: ActivityStatus): boolean {
	return status === 'queued' || status === 'running';
}

function filterActivityInternal(activity: Activity, filter: ActivityFilter): boolean {
	switch (filter) {
		case 'running':
			return isActiveStatusInternal(activity.status);
		case 'failed':
			return activity.status === 'failed';
		case 'completed':
			return activity.status === 'success' || activity.status === 'cancelled';
	}
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
	let _filter = $state<ActivityFilter>('running');
	let _open = $state(false);
	let _currentEnvironmentId = $state(LOCAL_DOCKER_ENVIRONMENT_ID);
	let sessionGeneration = 0;

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
	}

	function mergeActivityInternal(activity: Activity) {
		const environmentId = sourceEnvironmentIdInternal(activity);
		const normalized = normalizeActivityInternal(activity, environmentId);
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

	function environmentFailuresInternal(): ActivityEnvironmentFailure[] {
		return Object.values(core.environmentStates)
			.filter((state) => state.streamError)
			.map((state) => ({
				environmentId: state.id,
				environmentName: state.name,
				message: state.errorMessage
			}));
	}

	return {
		get activities(): Activity[] {
			return _activities;
		},
		get filteredActivities(): Activity[] {
			return _activities.filter((activity) => filterActivityInternal(activity, _filter));
		},
		get activeCount(): number {
			return _activities.filter((activity) => isActiveStatusInternal(activity.status)).length;
		},
		get filter(): ActivityFilter {
			return _filter;
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
				_activities = [];
				_environmentActivities = {};
				_details = {};
				_expandedActivityIds = {};
				_detailLoadingIds = {};
				_detailErrorIds = {};
				_cancellingIds = {};
				_filter = 'running';
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
		setFilter: (filter: ActivityFilter) => {
			_filter = filter;
		},
		setOpen: (open: boolean) => {
			_open = open;
		},
		openCenter: (activityId?: string) => {
			_open = true;
			if (activityId) {
				setActivityExpanded(activityId, true);
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
