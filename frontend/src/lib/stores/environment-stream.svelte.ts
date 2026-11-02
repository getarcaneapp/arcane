import { browser } from '$app/env';
import { untrack } from 'svelte';

import { clientStream } from '#lib/stores/client-stream.svelte.js';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import type { Environment } from '#lib/types/environment.js';
import { createAbortLifecycle } from '#lib/utils/abort-lifecycle.js';

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
	/** Used in console warnings, e.g. 'Activity'. */
	label: string;
	createEnvironmentState(environment: Pick<Environment, 'id' | 'name'>): TState;
	/** Channel to subscribe to on the shared client stream. */
	channel: string;
	/** Handles every event type except 'heartbeat' (core owns connection state). */
	applyEvent(environmentId: string, event: TEvent): void;
	fetchSnapshot(environmentId: string, signal: AbortSignal): Promise<void>;
	refreshOnStart?: boolean;
	/** Limits aggregate stream state and REST snapshots to caller-authorized environments. */
	includeEnvironment?(environment: Pick<Environment, 'id' | 'name'>): boolean;
	watchEnvironmentFilter?(): unknown;
	/** Extra cleanup when an environment disappears (core already dropped its state). */
	onEnvironmentRemoved?(environmentId: string): void;
	/** Replaces the default rename handling (which just updates state.name). */
	onEnvironmentRenamed?(environmentId: string, name: string): void;
	/** Extra fields reset whenever a per-environment error is cleared (e.g. { errorCode: undefined }). */
	clearErrorExtra?: Partial<TState>;
}

export function createEnvironmentStreamStore<TState extends StreamEnvStateBase, TEvent extends StreamEventBase>(
	config: EnvStreamCoreConfig<TState, TEvent>
) {
	let _environmentStates = $state.raw<Record<string, TState>>({});

	const lifecycle = createAbortLifecycle();
	let lifecycleSignal = AbortSignal.abort();

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

	async function refresh(signal = lifecycleSignal) {
		// The Promise.all below fetches every environment, including new ones.
		reconcileEnvironments(false);
		await Promise.all(Object.keys(_environmentStates).map((environmentId) => config.fetchSnapshot(environmentId, signal)));
	}

	function reconcileEnvironments(fetchNew = true) {
		if (!browser || !lifecycle.active) {
			return;
		}

		// Track only enabled environments — they are the ones the aggregated
		// stream serves; a disabled environment would never leave "loading".
		const included = (environment: Pick<Environment, 'id' | 'name'>) => config.includeEnvironment?.(environment) ?? true;
		const environments = environmentStore.available.filter((environment) => environment.enabled && included(environment));
		const targetIds = new Set(environments.map((environment) => environment.id || LOCAL_DOCKER_ENVIRONMENT_ID));

		for (const environmentId of Object.keys(_environmentStates)) {
			if (!targetIds.has(environmentId)) {
				removeEnvironment(environmentId);
			}
		}

		for (const environment of environments) reconcileEnvironment(environment, fetchNew);
	}

	function reconcileEnvironment(environment: Pick<Environment, 'id' | 'name'>, fetchNew: boolean) {
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
			if (fetchNew && clientStream.hasActiveStream) {
				void config.fetchSnapshot(environmentId, lifecycleSignal);
			}
			return;
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
		get hasActiveStream(): boolean {
			return clientStream.hasActiveStream;
		},
		environmentState,
		updateEnvironmentState,
		setEnvironmentError,
		clearEnvironmentError,
		reconcileEnvironments,
		refresh,
		start() {
			if (!browser || lifecycle.active) {
				return;
			}

			lifecycle.start((signal) => {
				lifecycleSignal = signal;
				void environmentStore.ready.then(() => {
					if (signal.aborted) {
						return;
					}
					if (config.refreshOnStart) {
						void refresh(signal);
					} else {
						reconcileEnvironments();
					}
					const unsubscribers = [
						clientStream.subscribe(config.channel, channelHandlers),
						environmentStore.subscribeAvailable(() => reconcileEnvironments()),
						$effect.root(() => {
							$effect(() => {
								config.watchEnvironmentFilter?.();
								untrack(() => reconcileEnvironments());
							});
						})
					];
					signal.addEventListener(
						'abort',
						() => {
							for (const unsubscribe of unsubscribers) unsubscribe();
						},
						{ once: true }
					);
				});
			});
		},
		stop(options?: { resetState?: boolean; resetStreamFailed?: boolean }) {
			const wasStarted = lifecycle.active;
			lifecycle.stop();
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
