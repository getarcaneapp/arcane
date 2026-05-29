import { browser } from '$app/environment';
import { activityService } from '$lib/services/activity-service';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
import type {
	Activity,
	ActivityDetail,
	ActivityFilter,
	ActivityMessage,
	ActivityStatus,
	ActivityStreamEvent
} from '$lib/types/activity.type';

const ACTIVITY_LIST_LIMIT = 50;
const ACTIVITY_DETAIL_LIMIT = 500;
const MAX_RECONNECT_DELAY = 15_000;
const MAX_RECONNECT_ATTEMPTS = 20;

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

function createActivityStore() {
	let _activities = $state<Activity[]>([]);
	let _details = $state<Record<string, ActivityDetail>>({});
	let _expandedActivityIds = $state<Record<string, boolean>>({});
	let _detailLoadingIds = $state<Record<string, boolean>>({});
	let _detailErrorIds = $state<Record<string, boolean>>({});
	let _cancellingIds = $state<Record<string, boolean>>({});
	let _filter = $state<ActivityFilter>('running');
	let _open = $state(false);
	let _loading = $state(false);
	let _connected = $state(false);
	let _streamError = $state(false);
	let _currentEnvironmentId = $state(LOCAL_DOCKER_ENVIRONMENT_ID);

	let started = false;
	let streamGeneration = 0;
	let streamAbortController: AbortController | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let unsubscribeEnvironment: (() => void) | null = null;
	let reconnectAttempt = 0;

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
		_connected = false;
	}

	function resetEnvironmentStateInternal(environmentId: string) {
		_currentEnvironmentId = environmentId || LOCAL_DOCKER_ENVIRONMENT_ID;
		_activities = [];
		_details = {};
		_expandedActivityIds = {};
		_detailLoadingIds = {};
		_detailErrorIds = {};
		_connected = false;
		_streamError = false;
		_loading = true;
		reconnectAttempt = 0;
	}

	async function refreshInternal(environmentId = _currentEnvironmentId, generation = streamGeneration) {
		_loading = true;
		try {
			const result = await activityService.getActivities({ pagination: { page: 1, limit: ACTIVITY_LIST_LIMIT } }, environmentId);
			if (generation !== streamGeneration || environmentId !== _currentEnvironmentId) {
				return;
			}
			replaceSnapshotInternal(result.data ?? []);
		} catch (error) {
			console.warn('Failed to refresh activities:', error);
		} finally {
			if (generation === streamGeneration && environmentId === _currentEnvironmentId) {
				_loading = false;
			}
		}
	}

	function replaceSnapshotInternal(activities: Activity[]) {
		_activities = sortActivitiesInternal(activities);
		// Drop expansion state for activities that no longer exist in the snapshot.
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
		const index = _activities.findIndex((item) => item.id === activity.id);
		if (index >= 0) {
			_activities = sortActivitiesInternal([..._activities.slice(0, index), activity, ..._activities.slice(index + 1)]);
		} else {
			_activities = sortActivitiesInternal([activity, ..._activities]).slice(0, ACTIVITY_LIST_LIMIT);
		}

		const existingDetail = _details[activity.id];
		if (existingDetail) {
			_details = {
				..._details,
				[activity.id]: {
					...existingDetail,
					activity
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

	function applyStreamEventInternal(event: ActivityStreamEvent) {
		switch (event.type) {
			case 'snapshot':
				replaceSnapshotInternal(event.activities ?? []);
				_loading = false;
				break;
			case 'activity':
				if (event.activity) {
					mergeActivityInternal(event.activity);
				}
				break;
			case 'message':
				if (event.message) {
					mergeMessageInternal(event.message);
				}
				break;
			case 'heartbeat':
				_connected = true;
				break;
		}
	}

	async function connectStreamInternal(environmentId: string, generation: number) {
		if (!browser || generation !== streamGeneration) {
			return;
		}

		streamAbortController = new AbortController();
		try {
			const response = await activityService.openActivityStream(environmentId, streamAbortController.signal, ACTIVITY_LIST_LIMIT);
			if (generation !== streamGeneration || !response.body) {
				streamAbortController = null;
				return;
			}

			_connected = true;
			_streamError = false;
			reconnectAttempt = 0;
			await readJSONLinesInternal(response.body, generation);
		} catch (error) {
			if (!streamAbortController?.signal.aborted && generation === streamGeneration) {
				console.warn('Activity stream disconnected:', error);
			}
		} finally {
			streamAbortController = null;
			if (generation === streamGeneration) {
				_connected = false;
				scheduleReconnectInternal(environmentId, generation);
			}
		}
	}

	async function readJSONLinesInternal(stream: ReadableStream<Uint8Array>, generation: number) {
		const reader = stream.getReader();
		const decoder = new TextDecoder();
		let buffer = '';

		try {
			while (generation === streamGeneration) {
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
			applyStreamEventInternal(JSON.parse(trimmed) as ActivityStreamEvent);
		} catch (error) {
			console.warn('Failed to parse activity stream line:', error);
		}
	}

	function scheduleReconnectInternal(environmentId: string, generation: number) {
		if (!browser || !started || generation !== streamGeneration) {
			return;
		}

		if (reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
			_streamError = true;
			return;
		}

		clearReconnectTimerInternal();
		const delay = Math.min(1000 * 2 ** reconnectAttempt, MAX_RECONNECT_DELAY);
		reconnectAttempt += 1;
		reconnectTimer = setTimeout(() => {
			void connectStreamInternal(environmentId, generation);
		}, delay);
	}

	function restartForEnvironmentInternal(environmentId: string) {
		streamGeneration += 1;
		abortStreamInternal();
		resetEnvironmentStateInternal(environmentId);
		const generation = streamGeneration;
		void refreshInternal(environmentId, generation);
		void connectStreamInternal(environmentId, generation);
	}

	async function loadDetailInternal(activityId: string) {
		if (_details[activityId] || _detailLoadingIds[activityId]) {
			return;
		}

		_detailLoadingIds = { ..._detailLoadingIds, [activityId]: true };
		try {
			const detail = await activityService.getActivity(activityId, _currentEnvironmentId, ACTIVITY_DETAIL_LIMIT);
			_details = { ..._details, [activityId]: detail };
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
			return _loading;
		},
		get connected(): boolean {
			return _connected;
		},
		get streamError(): boolean {
			return _streamError;
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
			restartForEnvironmentInternal(environmentStore.selected?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID);
			unsubscribeEnvironment = environmentStore.subscribeSelected((environment) => {
				restartForEnvironmentInternal(environment?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID);
			});
		},
		stop: () => {
			started = false;
			unsubscribeEnvironment?.();
			unsubscribeEnvironment = null;
			streamGeneration += 1;
			abortStreamInternal();
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
				await activityService.cancelActivity(activityId, _currentEnvironmentId);
			} finally {
				const next = { ..._cancellingIds };
				delete next[activityId];
				_cancellingIds = next;
			}
		},
		clearHistory: async () => {
			await activityService.clearHistory(_currentEnvironmentId);
			_details = {};
			_expandedActivityIds = {};
			_detailLoadingIds = {};
			_detailErrorIds = {};
			await refreshInternal();
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
			_streamError = false;
			reconnectAttempt = 0;
			restartForEnvironmentInternal(_currentEnvironmentId);
		},
		setActivityExpanded,
		toggleActivity
	};
}

export const activityStore = createActivityStore();
