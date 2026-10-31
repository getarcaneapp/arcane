import { browser } from '$app/env';

import { STREAM_CHANNEL_ENVIRONMENTS } from '#lib/services/stream-service.js';
import { clientStream } from '#lib/stores/client-stream.svelte.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { Environment } from '#lib/types/environment.js';
import { createAbortLifecycle } from '#lib/utils/abort-lifecycle.js';

type EnvironmentStreamEvent = {
	type: string;
	environments?: Environment[];
};

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

	return {
		get streamConnected(): boolean {
			return clientStream.streamConnected;
		},
		get streamFailed(): boolean {
			return clientStream.streamFailed;
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
