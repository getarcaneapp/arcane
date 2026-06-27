import { browser } from '$app/env';
import { dashboardService } from '$lib/services/dashboard-service';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
import type { DashboardSnapshot, DashboardStreamErrorCode, DashboardStreamEvent } from '$lib/types/shared';
import type { Environment } from '$lib/types/environment';

const MAX_RECONNECT_DELAY = 15_000;
const MAX_RECONNECT_ATTEMPTS = 20;

type DashboardEnvironmentState = {
	id: string;
	name: string;
	snapshot: DashboardSnapshot | null;
	// hasLoaded flips on the first-ever snapshot and never back: later errors
	// keep showing the last-known data instead of skeletons or zeros.
	hasLoaded: boolean;
	loading: boolean;
	streamError: boolean;
	errorMessage?: string;
	errorCode?: DashboardStreamErrorCode;
};

function environmentNameInternal(environment: Pick<Environment, 'id' | 'name'> | null | undefined): string {
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

function createDashboardStore() {
	let _environmentStates = $state<Record<string, DashboardEnvironmentState>>({});

	let started = false;
	let debugAllGood = false;
	let unsubscribeEnvironment: (() => void) | null = null;
	// A single aggregated stream carries every environment's snapshots; per-env
	// pollers would multiply requests and break against agents on older versions
	// without a per-environment error story.
	let streamAbortController: AbortController | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectAttempt = 0;
	let streamGeneration = 0;
	let _streamConnected = $state(false);
	let _streamFailed = $state(false);

	function createEnvironmentStateInternal(environment: Pick<Environment, 'id' | 'name'>): DashboardEnvironmentState {
		return {
			id: environment.id || LOCAL_DOCKER_ENVIRONMENT_ID,
			name: environmentNameInternal(environment),
			snapshot: null,
			hasLoaded: false,
			loading: true,
			streamError: false
		};
	}

	function environmentStateInternal(environmentId: string): DashboardEnvironmentState | undefined {
		return _environmentStates[environmentId];
	}

	function updateEnvironmentStateInternal(
		environmentId: string,
		updater: (state: DashboardEnvironmentState) => DashboardEnvironmentState
	) {
		const current =
			_environmentStates[environmentId] ?? createEnvironmentStateInternal({ id: environmentId, name: environmentId });
		_environmentStates = {
			..._environmentStates,
			[environmentId]: updater(current)
		};
	}

	function setEnvironmentErrorInternal(environmentId: string, error: unknown, errorCode?: DashboardStreamErrorCode) {
		// Errors only flag the state; snapshot and hasLoaded are left untouched
		// so the card keeps rendering the last-known values.
		updateEnvironmentStateInternal(environmentId, (state) => ({
			...state,
			loading: false,
			streamError: true,
			errorMessage: errorMessageInternal(error),
			errorCode
		}));
	}

	function clearEnvironmentErrorInternal(environmentId: string) {
		updateEnvironmentStateInternal(environmentId, (state) => ({
			...state,
			streamError: false,
			errorMessage: undefined,
			errorCode: undefined
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
	}

	function replaceEnvironmentSnapshotInternal(environmentId: string, snapshot: DashboardSnapshot) {
		// Snapshots can still arrive (stream or in-flight REST) after the
		// environment was removed locally; don't resurrect it.
		if (!environmentStateInternal(environmentId)) {
			return;
		}
		updateEnvironmentStateInternal(environmentId, (state) => ({
			...state,
			snapshot,
			hasLoaded: true,
			loading: false,
			streamError: false,
			errorMessage: undefined,
			errorCode: undefined
		}));
	}

	function applyStreamEventInternal(environmentId: string, event: DashboardStreamEvent) {
		// The aggregated stream can keep delivering events for an environment
		// for a short while after it was removed locally; don't resurrect it.
		if (event.type !== 'heartbeat' && !environmentStateInternal(environmentId)) {
			return;
		}
		switch (event.type) {
			case 'snapshot':
				if (event.snapshot) {
					replaceEnvironmentSnapshotInternal(environmentId, event.snapshot);
				}
				break;
			case 'pending':
				// The server confirms this environment is covered; the first
				// snapshot or error for it will follow.
				break;
			case 'heartbeat':
				_streamConnected = true;
				break;
			case 'error':
				setEnvironmentErrorInternal(environmentId, new Error(event.error || 'Dashboard stream error'), event.errorCode);
				break;
		}
	}

	async function refreshEnvironmentInternal(environmentId: string, generation = streamGeneration) {
		try {
			const snapshot = await dashboardService.getDashboardForEnvironment(environmentId, { debugAllGood });
			// The environment can be removed while the fetch is in-flight; don't resurrect it.
			if (!isCurrentGenerationInternal(generation) || !environmentStateInternal(environmentId)) {
				return;
			}
			replaceEnvironmentSnapshotInternal(environmentId, snapshot);
		} catch (error) {
			if (isCurrentGenerationInternal(generation) && environmentStateInternal(environmentId)) {
				console.warn('Failed to refresh dashboard snapshot:', error);
				setEnvironmentErrorInternal(environmentId, error);
			}
		}
	}

	async function refreshInternal(generation = streamGeneration) {
		reconcileEnvironmentsInternal();
		await Promise.all(Object.keys(_environmentStates).map((environmentId) => refreshEnvironmentInternal(environmentId, generation)));
	}

	async function connectStreamInternal(generation: number) {
		if (!browser || !isCurrentGenerationInternal(generation)) {
			return;
		}

		const controller = new AbortController();
		streamAbortController = controller;
		try {
			const response = await dashboardService.openDashboardStream(controller.signal, debugAllGood);
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
				console.warn('Dashboard stream disconnected:', error);
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
			const event = JSON.parse(trimmed) as DashboardStreamEvent;
			applyStreamEventInternal(event.environmentId || LOCAL_DOCKER_ENVIRONMENT_ID, event);
		} catch (error) {
			console.warn('Failed to parse dashboard stream line:', error);
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
				updateEnvironmentStateInternal(environmentId, (state) => ({
					...state,
					name: environmentNameInternal(environment)
				}));
			}
		}
	}

	return {
		get connected(): boolean {
			return _streamConnected;
		},
		get streamFailed(): boolean {
			return _streamFailed;
		},
		getEnvironmentState(environmentId: string): DashboardEnvironmentState | null {
			return _environmentStates[environmentId] ?? null;
		},
		isSnapshotLoading(environmentId: string): boolean {
			const state = _environmentStates[environmentId];
			return Boolean(state && state.loading && !state.hasLoaded);
		},
		start: async (options?: { debugAllGood?: boolean }) => {
			const nextDebugAllGood = options?.debugAllGood ?? false;
			if (!browser) {
				return;
			}
			if (started) {
				if (nextDebugAllGood === debugAllGood) {
					return;
				}
				// The flag is encoded in the stream URL; restart to apply it.
				debugAllGood = nextDebugAllGood;
				abortStreamInternal();
				void connectStreamInternal(nextGenerationInternal());
				return;
			}

			started = true;
			debugAllGood = nextDebugAllGood;
			await environmentStore.ready;
			reconcileEnvironmentsInternal();
			const generation = nextGenerationInternal();
			void refreshInternal(generation);
			void connectStreamInternal(generation);
			unsubscribeEnvironment = environmentStore.subscribeSelected(() => {
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
			_streamFailed = false;
		},
		refresh: () => refreshInternal(),
		retryStream: () => {
			_streamFailed = false;
			reconnectAttempt = 0;
			clearAllEnvironmentErrorsInternal();
			abortStreamInternal();
			void connectStreamInternal(nextGenerationInternal());
		}
	};
}

export const dashboardStore = createDashboardStore();
