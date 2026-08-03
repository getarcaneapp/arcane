import { browser } from '$app/env';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte';
import { clientStream } from '#lib/stores/client-stream.svelte';
import type { Environment } from '#lib/types/environment';

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
	/** Channel to subscribe to on the shared client stream. */
	channel: string;
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
	// REST snapshots belong to this store lifecycle, not the shared transport's reconnect lifecycle.
	let lifecycleGeneration = 0;
	let unsubscribeEnvironment: (() => void) | null = null;
	let unsubscribeEnvironmentFilter: (() => void) | null = null;

	let unsubscribeChannel: (() => void) | null = null;

	const channelHandlers = {
		// A fresh stream re-emits error events for environments that are still
		// failing, so stale per-environment errors are cleared on every (re)connect.
		onConnected: () => clearAllEnvironmentErrors(),
		onEvent: (payload: unknown) => {
			const event = payload as TEvent;
			const environmentId = event.environmentId || LOCAL_DOCKER_ENVIRONMENT_ID;
			// The aggregated stream can keep delivering events for an environment
			// for a short while after it was removed locally; don't resurrect it.
			if (!environmentState(environmentId)) {
				return;
			}
			config.applyEvent(environmentId, event);
		}
	};

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

	function removeEnvironment(environmentId: string) {
		const nextStates = { ..._environmentStates };
		delete nextStates[environmentId];
		_environmentStates = nextStates;
		config.onEnvironmentRemoved?.(environmentId);
	}

	async function refresh(generation = lifecycleGeneration) {
		reconcileEnvironments();
		await Promise.all(Object.keys(_environmentStates).map((environmentId) => config.fetchSnapshot(environmentId, generation)));
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
				if (clientStream.hasActiveStream) {
					void config.fetchSnapshot(environmentId, lifecycleGeneration);
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
			return clientStream.streamConnected;
		},
		get streamFailed(): boolean {
			return clientStream.streamFailed;
		},
		get generation(): number {
			return lifecycleGeneration;
		},
		get hasActiveStream(): boolean {
			return clientStream.hasActiveStream;
		},
		environmentState,
		updateEnvironmentState,
		setEnvironmentError,
		clearEnvironmentError,
		isCurrentGeneration: (generation: number) => generation === lifecycleGeneration,
		reconcileEnvironments,
		refresh,
		async start() {
			if (!browser || started) {
				return;
			}

			started = true;
			const generation = ++lifecycleGeneration;
			await environmentStore.ready;
			if (generation !== lifecycleGeneration) {
				return;
			}
			config.onSelectedEnvironment?.(environmentStore.selected);
			reconcileEnvironments();
			unsubscribeChannel = clientStream.subscribe(config.channel, channelHandlers);
			if (config.refreshOnStart) {
				void refresh(generation);
			}
			unsubscribeEnvironment = environmentStore.subscribeSelected((environment) => {
				config.onSelectedEnvironment?.(environment);
				reconcileEnvironments();
			});
			unsubscribeEnvironmentFilter = config.subscribeEnvironmentFilter?.(reconcileEnvironments) ?? null;
		},
		stop(options?: { resetState?: boolean; resetStreamFailed?: boolean }) {
			const wasStarted = started;
			started = false;
			lifecycleGeneration += 1;
			unsubscribeEnvironment?.();
			unsubscribeEnvironment = null;
			unsubscribeEnvironmentFilter?.();
			unsubscribeEnvironmentFilter = null;
			unsubscribeChannel?.();
			unsubscribeChannel = null;
			if (options?.resetState) {
				_environmentStates = {};
			}
			return wasStarted;
		},
		retryStream() {
			// Environments may have been added/removed while the stream was down;
			// reconcile so the new stream's snapshots aren't dropped as unknown.
			reconcileEnvironments();
			clearAllEnvironmentErrors();
			clientStream.retry();
		},
		// Tear down and reopen the stream without touching existing environment
		// data (e.g. when a flag encoded in the stream URL changes).
		restartStream() {
			// Same as retryStream: pick up environments added since the last
			// reconcile so their first snapshots aren't discarded.
			reconcileEnvironments();
			clientStream.restart();
		}
	};
}
