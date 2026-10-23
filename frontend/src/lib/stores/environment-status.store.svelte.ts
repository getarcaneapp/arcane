import { browser } from '$app/env';

import { STREAM_CHANNEL_ENVIRONMENTS } from '#lib/services/stream-service.js';
import { clientStream } from '#lib/stores/client-stream.svelte.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { Environment } from '#lib/types/environment.js';

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
 * frozen at whatever the root layout load happened to see.
 */
function createEnvironmentStatusStoreInternal() {
	let started = false;
	let lifecycleGeneration = 0;

	let unsubscribeChannels: (() => void)[] = [];

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
			unsubscribeChannels = [
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
		},
		stop() {
			const wasStarted = started;
			started = false;
			lifecycleGeneration += 1;
			for (const unsubscribe of unsubscribeChannels) unsubscribe();
			unsubscribeChannels = [];
			return wasStarted;
		}
	};
}

export const environmentStatusStore = createEnvironmentStatusStoreInternal();
