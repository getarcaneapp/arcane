import { browser } from '$app/env';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
import type { Environment } from '$lib/types/environment';

const MAX_RECONNECT_DELAY = 15_000;
const MAX_RECONNECT_ATTEMPTS = 20;

export type StreamEnvStateBase = {
	id: string;
	name: string;
	loading: boolean;
	streamError: boolean;
	errorMessage?: string;
};

export type StreamEventBase = {
	type: string;
	environmentId?: string;
};

export function environmentDisplayName(environment: Pick<Environment, 'id' | 'name'> | null | undefined): string {
	if (!environment) {
		return 'Local';
	}
	return environment.name || environment.id;
}

export function streamErrorMessage(error: unknown): string | undefined {
	if (error instanceof Error && error.message.trim()) {
		return error.message;
	}
	return undefined;
}

export interface EnvStreamCoreConfig<TState extends StreamEnvStateBase, TEvent extends StreamEventBase> {
	/** Used in console warnings, e.g. 'Dashboard' / 'Activity'. */
	label: string;
	createEnvironmentState(environment: Pick<Environment, 'id' | 'name'>): TState;
	openStream(signal: AbortSignal): Promise<Response>;
	/** Handles every event type except 'heartbeat' (core owns connection state). */
	applyEvent(environmentId: string, event: TEvent): void;
	/** Fully owns a per-environment REST refresh, including generation/removal guards via core helpers. */
	fetchSnapshot(environmentId: string, generation: number): Promise<void>;
	refreshOnStart?: boolean;
	/** Limits aggregate stream state and REST snapshots to caller-authorized environments. */
	includeEnvironment?(environment: Pick<Environment, 'id' | 'name'>): boolean;
	/** Reconciles when state used by includeEnvironment changes (for example, user permissions). */
	subscribeEnvironmentFilter?(reconcile: () => void): () => void;
	/** Extra cleanup when an environment disappears (core already dropped its state). */
	onEnvironmentRemoved?(environmentId: string): void;
	/** Replaces the default rename handling (which just updates state.name). */
	onEnvironmentRenamed?(environmentId: string, name: string): void;
	onSelectedEnvironment?(environment: Pick<Environment, 'id' | 'name'> | null | undefined): void;
	/** Extra fields reset whenever a per-environment error is cleared (e.g. { errorCode: undefined }). */
	clearErrorExtra?: Partial<TState>;
}

export function createEnvironmentStreamStore<TState extends StreamEnvStateBase, TEvent extends StreamEventBase>(
	config: EnvStreamCoreConfig<TState, TEvent>
) {
	let _environmentStates = $state<Record<string, TState>>({});

	let started = false;
	let unsubscribeEnvironment: (() => void) | null = null;
	let unsubscribeEnvironmentFilter: (() => void) | null = null;
	// A single aggregated stream carries every environment's events; per-env
	// connections would multiply requests and exhaust the browser's
	// 6-per-origin HTTP/1.1 limit.
	let streamAbortController: AbortController | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectAttempt = 0;
	let streamGeneration = 0;
	let _streamConnected = $state(false);
	let _streamFailed = $state(false);

	function environmentState(environmentId: string): TState | undefined {
		return _environmentStates[environmentId];
	}

	function updateEnvironmentState(environmentId: string, updater: (state: TState) => TState) {
		const current =
			_environmentStates[environmentId] ?? config.createEnvironmentState({ id: environmentId, name: environmentId });
		_environmentStates = {
			..._environmentStates,
			[environmentId]: updater(current)
		};
	}

	function setEnvironmentError(environmentId: string, error: unknown, extra?: Partial<TState>) {
		// Errors only flag the state; domain data is left untouched so the UI
		// keeps rendering the last-known values.
		updateEnvironmentState(environmentId, (state) => ({
			...state,
			loading: false,
			streamError: true,
			errorMessage: streamErrorMessage(error),
			...extra
		}));
	}

	function clearEnvironmentError(environmentId: string) {
		updateEnvironmentState(environmentId, (state) => ({
			...state,
			streamError: false,
			errorMessage: undefined,
			...config.clearErrorExtra
		}));
	}

	// A fresh stream re-emits error events for environments that are still
	// failing, so stale per-environment errors are cleared on every (re)connect.
	function clearAllEnvironmentErrors() {
		for (const environmentId of Object.keys(_environmentStates)) {
			if (environmentState(environmentId)?.streamError) {
				clearEnvironmentError(environmentId);
			}
		}
	}

	function nextGeneration(): number {
		streamGeneration += 1;
		return streamGeneration;
	}

	function isCurrentGeneration(generation: number): boolean {
		return streamGeneration === generation;
	}

	function clearReconnectTimer() {
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
	}

	function abortStream() {
		clearReconnectTimer();
		streamAbortController?.abort();
		streamAbortController = null;
		_streamConnected = false;
	}

	function removeEnvironment(environmentId: string) {
		const nextStates = { ..._environmentStates };
		delete nextStates[environmentId];
		_environmentStates = nextStates;
		config.onEnvironmentRemoved?.(environmentId);
	}

	async function refresh(generation = streamGeneration) {
		reconcileEnvironments();
		await Promise.all(Object.keys(_environmentStates).map((environmentId) => config.fetchSnapshot(environmentId, generation)));
	}

	async function connectStream(generation: number) {
		if (!browser || !isCurrentGeneration(generation)) {
			return;
		}

		const controller = new AbortController();
		streamAbortController = controller;
		try {
			const response = await config.openStream(controller.signal);
			if (!isCurrentGeneration(generation) || !response.body) {
				if (streamAbortController === controller) {
					streamAbortController = null;
				}
				return;
			}

			_streamConnected = true;
			_streamFailed = false;
			reconnectAttempt = 0;
			clearAllEnvironmentErrors();
			await readJSONLines(response.body, generation);
		} catch (error) {
			if (!controller.signal.aborted && isCurrentGeneration(generation)) {
				console.warn(`${config.label} stream disconnected:`, error);
			}
		} finally {
			if (streamAbortController === controller) {
				streamAbortController = null;
			}
			if (isCurrentGeneration(generation)) {
				_streamConnected = false;
				if (!controller.signal.aborted) {
					scheduleReconnect(generation);
				}
			}
		}
	}

	async function readJSONLines(stream: ReadableStream<Uint8Array>, generation: number) {
		const reader = stream.getReader();
		const decoder = new TextDecoder();
		let buffer = '';

		try {
			while (isCurrentGeneration(generation)) {
				const { done, value } = await reader.read();
				if (done) {
					break;
				}

				buffer += decoder.decode(value, { stream: true });
				const lines = buffer.split('\n');
				buffer = lines.pop() ?? '';
				for (const line of lines) {
					handleStreamLine(line);
				}
			}

			buffer += decoder.decode();
			if (buffer.trim()) {
				handleStreamLine(buffer);
			}
		} finally {
			reader.releaseLock();
		}
	}

	function handleStreamLine(line: string) {
		const trimmed = line.trim();
		if (!trimmed) {
			return;
		}

		try {
			const event = JSON.parse(trimmed) as TEvent;
			const environmentId = event.environmentId || LOCAL_DOCKER_ENVIRONMENT_ID;
			if (event.type === 'heartbeat') {
				_streamConnected = true;
				return;
			}
			// The aggregated stream can keep delivering events for an environment
			// for a short while after it was removed locally; don't resurrect it.
			if (!environmentState(environmentId)) {
				return;
			}
			config.applyEvent(environmentId, event);
		} catch (error) {
			console.warn(`Failed to parse ${config.label.toLowerCase()} stream line:`, error);
		}
	}

	function scheduleReconnect(generation: number) {
		if (!browser || !started || !isCurrentGeneration(generation)) {
			return;
		}

		if (reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
			_streamFailed = true;
			return;
		}

		clearReconnectTimer();
		const delay = Math.min(1000 * 2 ** reconnectAttempt, MAX_RECONNECT_DELAY);
		reconnectAttempt += 1;
		reconnectTimer = setTimeout(() => {
			void connectStream(generation);
		}, delay);
	}

	function reconcileEnvironments() {
		if (!browser || !started) {
			return;
		}

		// Track only enabled environments — they are the ones the aggregated
		// stream serves; a disabled environment would never leave "loading".
		const included = (environment: Pick<Environment, 'id' | 'name'>) => config.includeEnvironment?.(environment) ?? true;
		const available = environmentStore.available.filter((environment) => environment.enabled && included(environment));
		const selectedFallback = {
			id: environmentStore.selected?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID,
			name: environmentStore.selected?.name ?? 'Local'
		};
		const environments = available.length > 0 ? available : included(selectedFallback) ? [selectedFallback] : [];
		const targetIds = new Set(environments.map((environment) => environment.id || LOCAL_DOCKER_ENVIRONMENT_ID));

		for (const environmentId of Object.keys(_environmentStates)) {
			if (!targetIds.has(environmentId)) {
				removeEnvironment(environmentId);
			}
		}

		for (const environment of environments) {
			const environmentId = environment.id || LOCAL_DOCKER_ENVIRONMENT_ID;
			const existing = environmentState(environmentId);
			if (!existing) {
				_environmentStates = {
					..._environmentStates,
					[environmentId]: config.createEnvironmentState(environment)
				};
				// An already-open aggregated stream only picks new environments
				// up on its server-side reconcile tick; fetch once so the first
				// snapshot doesn't take up to that interval to appear.
				if (streamAbortController) {
					void config.fetchSnapshot(environmentId, streamGeneration);
				}
				continue;
			}

			if (existing.name !== environmentDisplayName(environment)) {
				if (config.onEnvironmentRenamed) {
					config.onEnvironmentRenamed(environmentId, environmentDisplayName(environment));
				} else {
					updateEnvironmentState(environmentId, (state) => ({
						...state,
						name: environmentDisplayName(environment)
					}));
				}
			}
		}
	}

	return {
		get environmentStates(): Record<string, TState> {
			return _environmentStates;
		},
		set environmentStates(value: Record<string, TState>) {
			_environmentStates = value;
		},
		get streamConnected(): boolean {
			return _streamConnected;
		},
		set streamConnected(value: boolean) {
			_streamConnected = value;
		},
		get streamFailed(): boolean {
			return _streamFailed;
		},
		get generation(): number {
			return streamGeneration;
		},
		get hasActiveStream(): boolean {
			return streamAbortController !== null;
		},
		environmentState,
		updateEnvironmentState,
		setEnvironmentError,
		clearEnvironmentError,
		isCurrentGeneration,
		reconcileEnvironments,
		refresh,
		async start() {
			if (!browser || started) {
				return;
			}

			started = true;
			await environmentStore.ready;
			if (!started) {
				return;
			}
			config.onSelectedEnvironment?.(environmentStore.selected);
			reconcileEnvironments();
			const generation = nextGeneration();
			if (config.refreshOnStart) {
				void refresh(generation);
			}
			void connectStream(generation);
			unsubscribeEnvironment = environmentStore.subscribeSelected((environment) => {
				config.onSelectedEnvironment?.(environment);
				reconcileEnvironments();
			});
			unsubscribeEnvironmentFilter = config.subscribeEnvironmentFilter?.(reconcileEnvironments) ?? null;
		},
		stop(options?: { resetState?: boolean; resetStreamFailed?: boolean }) {
			const wasStarted = started;
			started = false;
			unsubscribeEnvironment?.();
			unsubscribeEnvironment = null;
			unsubscribeEnvironmentFilter?.();
			unsubscribeEnvironmentFilter = null;
			nextGeneration();
			abortStream();
			reconnectAttempt = 0;
			if (options?.resetState) {
				_environmentStates = {};
			}
			if (options?.resetState || options?.resetStreamFailed) {
				_streamFailed = false;
			}
			return wasStarted;
		},
		retryStream() {
			_streamFailed = false;
			reconnectAttempt = 0;
			// Environments may have been added/removed while the stream was down;
			// reconcile so the new stream's snapshots aren't dropped as unknown.
			reconcileEnvironments();
			clearAllEnvironmentErrors();
			abortStream();
			void connectStream(nextGeneration());
		},
		// Tear down and reopen the stream without touching existing environment
		// data (e.g. when a flag encoded in the stream URL changes).
		restartStream() {
			// Same as retryStream: pick up environments added since the last
			// reconcile so their first snapshots aren't discarded.
			reconcileEnvironments();
			abortStream();
			void connectStream(nextGeneration());
		}
	};
}

export type EnvStreamCore<TState extends StreamEnvStateBase, TEvent extends StreamEventBase> = ReturnType<
	typeof createEnvironmentStreamStore<TState, TEvent>
>;
