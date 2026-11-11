import { browser } from '$app/env';

import { STREAM_CHANNEL_ENVIRONMENTS } from '#lib/services/stream-service.js';
import { clientStream } from '#lib/stores/client-stream.svelte.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { EnvironmentRuntimeStatus, EnvironmentStreamEvent } from '#lib/types/environment.js';
import type { PodmanStatus } from '#lib/types/podman.js';
import type { SwarmRuntimeStatus } from '#lib/types/swarm.js';
import { createAbortLifecycle } from '#lib/utils/abort-lifecycle.js';
import { readableEnvironments } from '#lib/utils/environments.js';

const RUNTIME_WAIT_LIMIT = 1_500;

/**
 * Keeps environmentStore's records live for as long as the app shell is mounted.
 *
 * Whether an agent environment is reachable lives only in the manager's
 * in-memory tunnel registry, so the browser can never work it out
 * on its own — it has to be told. Without this stream the environment list is
 * frozen at whatever the root layout load happened to see.
 */
function createEnvironmentStatusStoreInternal() {
	const lifecycle = createAbortLifecycle();
	let runtime = $state.raw<Record<string, EnvironmentRuntimeStatus>>({});
	let runtimeReceived = $state(false);
	const runtimeArrival = Promise.withResolvers<void>();

	function applyRuntime(statuses: EnvironmentRuntimeStatus[]) {
		runtime = Object.fromEntries(statuses.map((status) => [status.environmentId, status]));
		runtimeReceived = true;
		runtimeArrival.resolve();
	}

	return {
		get streamConnected(): boolean {
			return clientStream.streamConnected;
		},
		get streamFailed(): boolean {
			return clientStream.streamFailed;
		},
		get runtimeReceived(): boolean {
			return runtimeReceived;
		},
		get podmanStatuses(): Record<string, PodmanStatus> {
			return Object.fromEntries(
				Object.values(runtime).flatMap(({ environmentId, podman }) =>
					podman?.detected ? [[environmentId, podman] as const] : []
				)
			);
		},
		get podmanUnavailableReasons(): Record<string, string> {
			return Object.fromEntries(
				Object.values(runtime).flatMap(({ environmentId, podman }) =>
					podman && !podman.detected && podman.reason ? [[environmentId, podman.reason] as const] : []
				)
			);
		},
		get swarmEnabled(): boolean | undefined {
			const statuses = readableEnvironments('swarm:read').flatMap((env) => runtime[env.id]?.swarm ?? []);
			return statuses.length === 0 ? undefined : statuses.some((status) => status.enabled);
		},
		swarmStatus(environmentId: string): SwarmRuntimeStatus | undefined {
			return runtime[environmentId]?.swarm;
		},
		runtimeReady(): Promise<void> {
			let timeout: ReturnType<typeof setTimeout> | undefined;
			return Promise.race([
				runtimeArrival.promise,
				new Promise<void>((resolve) => (timeout = setTimeout(resolve, RUNTIME_WAIT_LIMIT)))
			]).finally(() => clearTimeout(timeout));
		},
		start() {
			if (!browser || lifecycle.active) {
				return;
			}
			lifecycle.start((signal) => {
				void environmentStore.ready.then(() => {
					if (signal.aborted) {
						return;
					}
					const unsubscribers = [
						clientStream.subscribe(STREAM_CHANNEL_ENVIRONMENTS, {
							onEvent: (payload) => {
								const event = payload as EnvironmentStreamEvent;
								if (event.type === 'runtime') {
									applyRuntime(event.runtime ?? []);
									return;
								}
								if (event.type !== 'snapshot' || !event.environments) {
									return;
								}
								for (const environmentId of environmentStore.applyLiveEnvironments(event.environments)) {
									void featureStore.refresh(environmentId);
									clientStream.invalidateEnvironment(environmentId);
								}
							}
						}),
						clientStream.watchChanges()
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
		stop() {
			const wasStarted = lifecycle.active;
			lifecycle.stop();
			return wasStarted;
		}
	};
}

export const environmentStatusStore = createEnvironmentStatusStoreInternal();
