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
	connected: boolean;
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
	const streamAbortControllers = new Map<string, AbortController>();
	const reconnectTimers = new Map<string, ReturnType<typeof setTimeout>>();
	const reconnectAttempts = new Map<string, number>();
	const streamGenerations = new Map<string, number>();

	function createEnvironmentStateInternal(environment: Pick<Environment, 'id' | 'name'>): ActivityEnvironmentState {
		return {
			id: environment.id || LOCAL_DOCKER_ENVIRONMENT_ID,
			name: environmentNameInternal(environment),
			activities: [],
			loading: true,
			connected: false,
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
			connected: false,
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

	function generationInternal(environmentId: string): number {
		return streamGenerations.get(environmentId) ?? 0;
	}

	function nextGenerationInternal(environmentId: string): number {
		const generation = generationInternal(environmentId) + 1;
		streamGenerations.set(environmentId, generation);
		return generation;
	}

	function isCurrentGenerationInternal(environmentId: string, generation: number): boolean {
		return generationInternal(environmentId) === generation;
	}

	function clearReconnectTimerInternal(environmentId: string) {
		const timer = reconnectTimers.get(environmentId);
		if (timer) {
			clearTimeout(timer);
			reconnectTimers.delete(environmentId);
		}
	}

	function abortEnvironmentStreamInternal(environmentId: string) {
		clearReconnectTimerInternal(environmentId);
		streamAbortControllers.get(environmentId)?.abort();
		streamAbortControllers.delete(environmentId);
		updateEnvironmentStateInternal(environmentId, (state) => ({
			...state,
			connected: false
		}));
	}

	function stopEnvironmentInternal(environmentId: string) {
		nextGenerationInternal(environmentId);
		abortEnvironmentStreamInternal(environmentId);
		reconnectAttempts.delete(environmentId);
		streamGenerations.delete(environmentId);

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
				updateEnvironmentStateInternal(environmentId, (state) => ({
					...state,
					connected: true
				}));
				break;
		}
	}

	async function refreshEnvironmentInternal(environmentId: string, generation = generationInternal(environmentId)) {
		updateEnvironmentStateInternal(environmentId, (state) => ({
			...state,
			loading: true
		}));
		try {
			const result = await activityService.getActivities({ pagination: { page: 1, limit: ACTIVITY_LIST_LIMIT } }, environmentId);
			if (!isCurrentGenerationInternal(environmentId, generation)) {
				return;
			}
			replaceEnvironmentSnapshotInternal(environmentId, result.data ?? []);
		} catch (error) {
			if (isCurrentGenerationInternal(environmentId, generation)) {
				console.warn('Failed to refresh activities:', error);
				setEnvironmentErrorInternal(environmentId, error);
			}
		}
	}

	async function refreshInternal() {
		reconcileEnvironmentsInternal();
		await Promise.all(Object.keys(_environmentStates).map((environmentId) => refreshEnvironmentInternal(environmentId)));
	}

	async function connectStreamInternal(environmentId: string, generation: number) {
		if (!browser || !isCurrentGenerationInternal(environmentId, generation)) {
			return;
		}

		const controller = new AbortController();
		streamAbortControllers.set(environmentId, controller);
		try {
			const response = await activityService.openActivityStream(environmentId, controller.signal, ACTIVITY_LIST_LIMIT);
			if (!isCurrentGenerationInternal(environmentId, generation) || !response.body) {
				if (streamAbortControllers.get(environmentId) === controller) {
					streamAbortControllers.delete(environmentId);
				}
				return;
			}

			updateEnvironmentStateInternal(environmentId, (state) => ({
				...state,
				connected: true,
				streamError: false,
				errorMessage: undefined
			}));
			reconnectAttempts.set(environmentId, 0);
			await readJSONLinesInternal(environmentId, response.body, generation);
		} catch (error) {
			if (!controller.signal.aborted && isCurrentGenerationInternal(environmentId, generation)) {
				console.warn('Activity stream disconnected:', error);
			}
		} finally {
			if (streamAbortControllers.get(environmentId) === controller) {
				streamAbortControllers.delete(environmentId);
			}
			if (isCurrentGenerationInternal(environmentId, generation)) {
				updateEnvironmentStateInternal(environmentId, (state) => ({
					...state,
					connected: false
				}));
				if (!controller.signal.aborted) {
					scheduleReconnectInternal(environmentId, generation);
				}
			}
		}
	}

	async function readJSONLinesInternal(environmentId: string, stream: ReadableStream<Uint8Array>, generation: number) {
		const reader = stream.getReader();
		const decoder = new TextDecoder();
		let buffer = '';

		try {
			while (isCurrentGenerationInternal(environmentId, generation)) {
				const { done, value } = await reader.read();
				if (done) {
					break;
				}

				buffer += decoder.decode(value, { stream: true });
				const lines = buffer.split('\n');
				buffer = lines.pop() ?? '';
				for (const line of lines) {
					handleStreamLineInternal(environmentId, line);
				}
			}

			buffer += decoder.decode();
			if (buffer.trim()) {
				handleStreamLineInternal(environmentId, buffer);
			}
		} finally {
			reader.releaseLock();
		}
	}

	function handleStreamLineInternal(environmentId: string, line: string) {
		const trimmed = line.trim();
		if (!trimmed) {
			return;
		}

		try {
			applyStreamEventInternal(environmentId, JSON.parse(trimmed) as ActivityStreamEvent);
		} catch (error) {
			console.warn('Failed to parse activity stream line:', error);
		}
	}

	function scheduleReconnectInternal(environmentId: string, generation: number) {
		if (!browser || !started || !isCurrentGenerationInternal(environmentId, generation)) {
			return;
		}

		const attempt = reconnectAttempts.get(environmentId) ?? 0;
		if (attempt >= MAX_RECONNECT_ATTEMPTS) {
			setEnvironmentErrorInternal(environmentId, new Error('Activity stream reconnect attempts exhausted'));
			return;
		}

		clearReconnectTimerInternal(environmentId);
		const delay = Math.min(1000 * 2 ** attempt, MAX_RECONNECT_DELAY);
		reconnectAttempts.set(environmentId, attempt + 1);
		reconnectTimers.set(
			environmentId,
			setTimeout(() => {
				void connectStreamInternal(environmentId, generation);
			}, delay)
		);
	}

	function startEnvironmentInternal(environment: Pick<Environment, 'id' | 'name'>) {
		const environmentId = environment.id || LOCAL_DOCKER_ENVIRONMENT_ID;
		updateEnvironmentStateInternal(environmentId, (state) => ({
			...state,
			id: environmentId,
			name: environmentNameInternal(environment)
		}));

		const generation = nextGenerationInternal(environmentId);
		reconnectAttempts.set(environmentId, 0);
		abortEnvironmentStreamInternal(environmentId);
		void refreshEnvironmentInternal(environmentId, generation);
		void connectStreamInternal(environmentId, generation);
	}

	function restartEnvironmentInternal(environmentId: string) {
		const state = environmentStateInternal(environmentId);
		if (!state) {
			return;
		}

		clearEnvironmentErrorInternal(environmentId);
		startEnvironmentInternal({ id: state.id, name: state.name });
	}

	function reconcileEnvironmentsInternal() {
		if (!browser || !started) {
			return;
		}

		const available = environmentStore.available;
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
				stopEnvironmentInternal(environmentId);
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
				startEnvironmentInternal(environment);
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
			return Object.values(_environmentStates).some((state) => state.connected);
		},
		get streamError(): boolean {
			return Object.values(_environmentStates).some((state) => state.streamError);
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
			unsubscribeEnvironment = environmentStore.subscribeSelected((environment) => {
				_currentEnvironmentId = environment?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID;
				reconcileEnvironmentsInternal();
			});
		},
		stop: () => {
			started = false;
			unsubscribeEnvironment?.();
			unsubscribeEnvironment = null;

			for (const environmentId of Object.keys(_environmentStates)) {
				nextGenerationInternal(environmentId);
				abortEnvironmentStreamInternal(environmentId);
			}
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
			for (const environmentId of Object.keys(_environmentStates)) {
				const state = environmentStateInternal(environmentId);
				if (state?.streamError) {
					restartEnvironmentInternal(environmentId);
				}
			}
		},
		setActivityExpanded,
		toggleActivity
	};
}

export const activityStore = createActivityStore();
