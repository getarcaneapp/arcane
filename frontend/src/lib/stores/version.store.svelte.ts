import { browser } from '$app/env';

import { STREAM_CHANNEL_VERSION } from '#lib/services/stream-service.js';
import { toAppVersionInformation } from '#lib/services/version-service.js';
import { clientStream } from '#lib/stores/client-stream.svelte.js';
import type { AppVersionInformation } from '#lib/types/settings.js';

type VersionStreamEvent = {
	type: string;
	info?: Partial<AppVersionInformation>;
};

function createVersionStoreInternal() {
	let current = $state.raw<AppVersionInformation | undefined>();
	let loaded = $state.raw<AppVersionInformation | undefined>();
	const buildChanged = $derived(
		!!loaded?.currentVersion &&
			!!current?.currentVersion &&
			(current.currentVersion !== loaded.currentVersion || current.revision !== loaded.revision)
	);
	let snapshots = $state(0);
	let started = false;
	let unsubscribeChannel: (() => void) | null = null;

	return {
		get current(): AppVersionInformation | undefined {
			return current;
		},
		get snapshots(): number {
			return snapshots;
		},
		get buildChanged(): boolean {
			return buildChanged;
		},
		seed(info: AppVersionInformation) {
			loaded ??= info;
			current = info;
		},
		start() {
			if (!browser || started) {
				return;
			}
			started = true;
			unsubscribeChannel = clientStream.subscribe(STREAM_CHANNEL_VERSION, {
				onEvent: (payload) => {
					const event = payload as VersionStreamEvent;
					if (event.type !== 'snapshot' || !event.info) {
						return;
					}
					current = toAppVersionInformation(event.info);
					snapshots++;
				}
			});
		},
		stop() {
			const wasStarted = started;
			started = false;
			unsubscribeChannel?.();
			unsubscribeChannel = null;
			return wasStarted;
		}
	};
}

export const versionStore = createVersionStoreInternal();
