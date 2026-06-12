import { browser } from '$app/env';
import { activityService } from '$lib/services/activity-service';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
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

const ACTIVITY_LIST_LIMIT = 50;
const ACTIVITY_DETAIL_LIMIT = 500;
const MAX_RECONNECT_DELAY = 15_000;
const MAX_RECONNECT_ATTEMPTS = 20;

type ActivityEnvironmentState = {
	id: string;
	name: string;
	activities: Activity[];
	loading: boolean;
	streamError: boolean;
	errorMessage?: string;
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

function environmentNameInternal(
	environment: Pick<Environment, 'id' | 'name'> | ActivityEnvironmentState | null | undefined
): string {
	if (!environment) {
		return 'Local';
	}
	return environment.name || environment.id;
}

function errorMessageInternal(error: unknown): string | undefined {
	if (error instanceof Error && error.message.trim()) {
		return error.message;
	}
	return undefined;
}

function createActivityStore() {
	let _activities = $state<Activity[]>([]);
	let _environmentStates = $state<Record<string, ActivityEnvironmentState>>({});
	let _environmentActivities = $state<Record<string, Activity[]>>({});
	let _details = $state<Record<string, ActivityDetail>>({});
	let _expandedActivityIds = $state<Record<string, boolean>>({});
	let _detailLoadingIds = $state<Record<string, boolean>>({});
	let _detailErrorIds = $state<Record<string, boolean>>({});
	let _cancellingIds = $state<Record<string, boolean>>({});
	let _filter = $state<ActivityFilter>('running');
	let _open = $state(false);
	let _currentEnvironmentId = $state(LOCAL_DOCKER_ENVIRONMENT_ID);

	let started = false;
	let unsubscribeEnvironment: (() => void) | null = null;
	// A single aggregated stream carries every environment's events; per-env
	// connections would exhaust the browser's 6-per-origin HTTP/1.1 limit.
	let streamAbortController: AbortController | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectAttempt = 0;
	let streamGeneration = 0;
	let _streamConnected = $state(false);
	let _streamFailed = $state(false);

	function createEnvironmentStateInternal(environment: Pick<Environment, 'id' | 'name'>): ActivityEnvironmentState {
		return {
			id: environment.id || LOCAL_DOCKER_ENVIRONMENT_ID,
			name: environmentNameInternal(environment),
			activities: [],
			loading: true,
			streamError: false
		};
	}

	function environmentStateInternal(environmentId: string): ActivityEnvironmentState | undefined {
		return _environmentStates[environmentId];
	}

	function updateEnvironmentStateInternal(
		environmentId: string,
		updater: (state: ActivityEnvironmentState) => ActivityEnvironmentState
	) {
		const current =
			_environmentStates[environmentId] ?? createEnvironmentStateInternal({ id: environmentId, name: environmentId });
		_environmentStates = {
			..._environmentStates,
			[environmentId]: updater(current)
		};
	}

	function setEnvironmentErrorInternal(environmentId: string, error: unknown) {
		updateEnvironmentStateInternal(environmentId, (state) => ({
			...state,
			loading: false,
			streamError: true,
			errorMessage: errorMessageInternal(error)
		}));
	}

	function clearEnvironmentErrorInternal(environmentId: string) {
		updateEnvironmentStateInternal(environmentId, (state) => ({
			...state,
			streamError: false,
			errorMessage: undefined
		}));
	}

	// A fresh stream re-emits error events for environments that are still
	// failing, so stale per-environment errors are cleared on every (re)connect.
	function clearAllEnvironmentErrorsInternal() {
		for (const environmentId of Object.keys(_environmentStates)) {
			if (environmentStateInternal(environmentId)?.streamError) {
				clearEnvironmentErrorInternal(environmentId);
			}
		}
	}

	function nextGenerationInternal(): number {
		streamGeneration += 1;
		return streamGeneration;
	}

	function isCurrentGenerationInternal(generation: number): boolean {
		return streamGeneration === generation;
	}

	function clearReconnectTimerInternal() {
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
	}

	function abortStreamInternal() {
		clearReconnectTimerInternal();
		streamAbortController?.abort();
		streamAbortController = null;
		_streamConnected = false;
	}

	function removeEnvironmentInternal(environmentId: string) {
		const nextStates = { ..._environmentStates };
		delete nextStates[environmentId];
		_environmentStates = nextStates;
		const nextActivities = { ..._environmentActivities };
		delete nextActivities[environmentId];
		_environmentActivities = nextActivities;
		rebuildActivitiesInternal();
	}

	function normalizeActivityInternal(activity: Activity, environmentId: string): Activity {
		const state = environmentStateInternal(environmentId);
		return {
			...activity,
			sourceEnvironmentId: activity.sourceEnvironmentId || environmentId,
			sourceEnvironmentName: activity.sourceEnvironmentName || state?.name || environmentId
		};
	}

	function replaceEnvironmentSnapshotInternal(environmentId: string, activities: Activity[]) {
		// Snapshots can still arrive (stream or in-flight REST) after the
		// environment was removed locally; don't resurrect it.
		if (!environmentStateInternal(environmentId)) {
			return;
		}
		const normalizedActivities = sortActivitiesInternal(
			activities.map((activity) => normalizeActivityInternal(activity, environmentId))
		);
		_environmentActivities = {
			..._environmentActivities,
			[environmentId]: normalizedActivities
		};
		updateEnvironmentStateInternal(environmentId, (state) => ({
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
		const currentActivities = _environmentActivities[environmentId] ?? environmentStateInternal(environmentId)?.activities ?? [];
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
		updateEnvironmentStateInternal(environmentId, (state) => {
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

	function applyStreamEventInternal(environmentId: string, event: ActivityStreamEvent) {
		// The aggregated stream can keep delivering events for an environment
		// for a short while after it was removed locally; don't resurrect it.
		if (event.type !== 'heartbeat' && !environmentStateInternal(environmentId)) {
			return;
		}
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
			case 'heartbeat':
				_streamConnected = true;
				break;
			case 'error':
				setEnvironmentErrorInternal(environmentId, new Error(event.error || 'Activity stream error'));
				break;
		}
	}

	async function refreshEnvironmentInternal(environmentId: string, generation = streamGeneration) {
		updateEnvironmentStateInternal(environmentId, (state) => ({
			...state,
			loading: true
		}));
		try {
			const result = await activityService.getActivities({ pagination: { page: 1, limit: ACTIVITY_LIST_LIMIT } }, environmentId);
			// The environment can be removed while the fetch is in-flight; don't resurrect it.
			if (!isCurrentGenerationInternal(generation) || !environmentStateInternal(environmentId)) {
				return;
			}
			replaceEnvironmentSnapshotInternal(environmentId, result.data ?? []);
		} catch (error) {
			if (isCurrentGenerationInternal(generation) && environmentStateInternal(environmentId)) {
				console.warn('Failed to refresh activities:', error);
				setEnvironmentErrorInternal(environmentId, error);
			}
		}
	}

	async function refreshInternal() {
		reconcileEnvironmentsInternal();
		await Promise.all(Object.keys(_environmentStates).map((environmentId) => refreshEnvironmentInternal(environmentId)));
	}

	async function connectStreamInternal(generation: number) {
		if (!browser || !isCurrentGenerationInternal(generation)) {
			return;
		}

		const controller = new AbortController();
		streamAbortController = controller;
		try {
			const response = await activityService.openActivityStream(controller.signal, ACTIVITY_LIST_LIMIT);
			if (!isCurrentGenerationInternal(generation) || !response.body) {
				if (streamAbortController === controller) {
					streamAbortController = null;
				}
				return;
			}

			_streamConnected = true;
			_streamFailed = false;
			reconnectAttempt = 0;
			clearAllEnvironmentErrorsInternal();
			await readJSONLinesInternal(response.body, generation);
		} catch (error) {
			if (!controller.signal.aborted && isCurrentGenerationInternal(generation)) {
				console.warn('Activity stream disconnected:', error);
			}
		} finally {
			if (streamAbortController === controller) {
				streamAbortController = null;
			}
			if (isCurrentGenerationInternal(generation)) {
				_streamConnected = false;
				if (!controller.signal.aborted) {
					scheduleReconnectInternal(generation);
				}
			}
		}
	}

	async function readJSONLinesInternal(stream: ReadableStream<Uint8Array>, generation: number) {
		const reader = stream.getReader();
		const decoder = new TextDecoder();
		let buffer = '';

		try {
			while (isCurrentGenerationInternal(generation)) {
				const { done, value } = await reader.read();
				if (done) {
					break;
				}

				buffer += decoder.decode(value, { stream: true });
				const lines = buffer.split('\n');
				buffer = lines.pop() ?? '';
				for (const line of lines) {
					handleStreamLineInternal(line);
				}
			}

			buffer += decoder.decode();
			if (buffer.trim()) {
				handleStreamLineInternal(buffer);
			}
		} finally {
			reader.releaseLock();
		}
	}

	function handleStreamLineInternal(line: string) {
		const trimmed = line.trim();
		if (!trimmed) {
			return;
		}

		try {
			const event = JSON.parse(trimmed) as ActivityStreamEvent;
			applyStreamEventInternal(event.environmentId || LOCAL_DOCKER_ENVIRONMENT_ID, event);
		} catch (error) {
			console.warn('Failed to parse activity stream line:', error);
		}
	}

	function scheduleReconnectInternal(generation: number) {
		if (!browser || !started || !isCurrentGenerationInternal(generation)) {
			return;
		}

		if (reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
			_streamFailed = true;
			return;
		}

		clearReconnectTimerInternal();
		const delay = Math.min(1000 * 2 ** reconnectAttempt, MAX_RECONNECT_DELAY);
		reconnectAttempt += 1;
		reconnectTimer = setTimeout(() => {
			void connectStreamInternal(generation);
		}, delay);
	}

	function reconcileEnvironmentsInternal() {
		if (!browser || !started) {
			return;
		}

		// Track only enabled environments — they are the ones the aggregated
		// stream serves; a disabled environment would never leave "loading".
		const available = environmentStore.available.filter((environment) => environment.enabled);
		const environments =
			available.length > 0
				? available
				: [
						{
							id: environmentStore.selected?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID,
							name: environmentStore.selected?.name ?? 'Local'
						}
					];
		const targetIds = new Set(environments.map((environment) => environment.id || LOCAL_DOCKER_ENVIRONMENT_ID));

		for (const environmentId of Object.keys(_environmentStates)) {
			if (!targetIds.has(environmentId)) {
				removeEnvironmentInternal(environmentId);
			}
		}

		for (const environment of environments) {
			const environmentId = environment.id || LOCAL_DOCKER_ENVIRONMENT_ID;
			const existing = environmentStateInternal(environmentId);
			if (!existing) {
				_environmentStates = {
					..._environmentStates,
					[environmentId]: createEnvironmentStateInternal(environment)
				};
				// An already-open aggregated stream only picks new environments
				// up on its server-side reconcile tick; fetch once so the first
				// snapshot doesn't take up to that interval to appear.
				if (streamAbortController) {
					void refreshEnvironmentInternal(environmentId);
				}
				continue;
			}

			if (existing.name !== environmentNameInternal(environment)) {
				const updatedActivities = (_environmentActivities[environmentId] ?? existing.activities).map((activity) =>
					activity.sourceEnvironmentName
						? activity
						: {
								...activity,
								sourceEnvironmentName: environmentNameInternal(environment)
							}
				);
				_environmentActivities = {
					..._environmentActivities,
					[environmentId]: updatedActivities
				};
				updateEnvironmentStateInternal(environmentId, (state) => ({
					...state,
					name: environmentNameInternal(environment),
					activities: updatedActivities
				}));
				rebuildActivitiesInternal();
			}
		}
	}

	function activityEnvironmentIdInternal(activityId: string): string {
		const activity = _details[activityId]?.activity ?? _activities.find((item) => item.id === activityId) ?? null;
		return sourceEnvironmentIdInternal(activity) || _currentEnvironmentId || LOCAL_DOCKER_ENVIRONMENT_ID;
	}

	async function loadDetailInternal(activityId: string) {
		if (_details[activityId] || _detailLoadingIds[activityId]) {
			return;
		}

		_detailLoadingIds = { ..._detailLoadingIds, [activityId]: true };
		try {
			const detail = await activityService.getActivity(
				activityId,
				activityEnvironmentIdInternal(activityId),
				ACTIVITY_DETAIL_LIMIT
			);
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
			console.warn('Failed to load activity detail:', error);
			_detailErrorIds = { ..._detailErrorIds, [activityId]: true };
		} finally {
			const next = { ..._detailLoadingIds };
			delete next[activityId];
			_detailLoadingIds = next;
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
		return Object.values(_environmentStates)
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
			return Object.values(_environmentStates).some((state) => state.loading);
		},
		get connected(): boolean {
			return _streamConnected;
		},
		get streamError(): boolean {
			return _streamFailed || Object.values(_environmentStates).some((state) => state.streamError);
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
		start: async () => {
			if (!browser || started) {
				return;
			}

			started = true;
			await environmentStore.ready;
			_currentEnvironmentId = environmentStore.selected?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID;
			reconcileEnvironmentsInternal();
			void connectStreamInternal(nextGenerationInternal());
			unsubscribeEnvironment = environmentStore.subscribeSelected((environment) => {
				_currentEnvironmentId = environment?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID;
				reconcileEnvironmentsInternal();
			});
		},
		stop: () => {
			started = false;
			unsubscribeEnvironment?.();
			unsubscribeEnvironment = null;
			nextGenerationInternal();
			abortStreamInternal();
			reconnectAttempt = 0;
		},
		refresh: () => refreshInternal(),
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
			reconcileEnvironmentsInternal();

			let deleted = 0;
			let succeeded = 0;
			const failed: ActivityEnvironmentFailure[] = [];
			const states = Object.values(_environmentStates);
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
							message: errorMessageInternal(error)
						});
					}
				})
			);

			_details = {};
			_expandedActivityIds = {};
			_detailLoadingIds = {};
			_detailErrorIds = {};
			await refreshInternal();

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
		retryStream: () => {
			_streamFailed = false;
			reconnectAttempt = 0;
			clearAllEnvironmentErrorsInternal();
			abortStreamInternal();
			void connectStreamInternal(nextGenerationInternal());
		},
		setActivityExpanded,
		toggleActivity
	};
}

export const activityStore = createActivityStore();
