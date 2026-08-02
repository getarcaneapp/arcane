import { browser } from '$app/env';
import { STREAM_CHANNEL_ENVIRONMENTS } from '#lib/services/stream-service';
import { clientStream } from '#lib/stores/client-stream.svelte';
import { environmentStore } from '#lib/stores/environment.store.svelte';
import type { Environment } from '#lib/types/environment';

type EnvironmentStreamEvent = {
	type: string;
	environments?: Environment[];
};

/**
 * Keeps environmentStore's records live for as long as the app shell is mounted.
 *
 * Whether an edge environment is reachable lives only in the manager's
 * in-memory tunnel and poll registries, so the browser can never work it out
 * on its own — it has to be told. Without this stream the environment list is
 * frozen at whatever the root layout load happened to see, which after a fleet
 * update is "every agent offline".
 */
function createEnvironmentStatusStoreInternal() {
	let started = false;
	let lifecycleGeneration = 0;

	let unsubscribeChannel: (() => void) | null = null;

	return {
		get streamConnected(): boolean {
			return clientStream.streamConnected;
		},
		get streamFailed(): boolean {
			return clientStream.streamFailed;
		},
		async start() {
			if (!browser || started) {
				return;
			}
			started = true;
			const generation = ++lifecycleGeneration;
			// The first snapshot replaces the list, so wait for the load-time
			// list to land or the two would race on an empty store.
			await environmentStore.ready;
			if (generation !== lifecycleGeneration) {
				return;
			}
			unsubscribeChannel = clientStream.subscribe(STREAM_CHANNEL_ENVIRONMENTS, {
				onEvent: (payload) => {
					const event = payload as EnvironmentStreamEvent;
					if (event.type !== 'snapshot' || !event.environments) {
						return;
					}
					environmentStore.applyLiveEnvironments(event.environments);
				}
			});
		},
		stop() {
			const wasStarted = started;
			started = false;
			lifecycleGeneration += 1;
			unsubscribeChannel?.();
			unsubscribeChannel = null;
			return wasStarted;
		}
	};
}

export const environmentStatusStore = createEnvironmentStatusStoreInternal();
