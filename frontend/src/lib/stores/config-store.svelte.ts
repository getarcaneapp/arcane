import { untrack } from 'svelte';

import { settingsService } from '#lib/services/settings-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { Settings } from '#lib/types/settings.js';

let current = $state.raw<Settings>();
const listeners = new Set<(settings: Settings | undefined) => void>();

const reload = async () => {
	const environmentId = await environmentStore.getCurrentEnvironmentId();
	const settings = await settingsService.getReadableSettings(environmentId);
	if (environmentId === environmentStore.selected?.id) set(settings);
};

const set = (settings: Settings) => {
	current = settings;
	untrack(() => {
		for (const listener of listeners) listener(settings);
	});
};

let autoLoginEnabled = $state(false);

export default {
	get current() {
		return current;
	},
	onChange(listener: (settings: Settings | undefined) => void) {
		listeners.add(listener);
		untrack(() => listener(current));
		return () => {
			listeners.delete(listener);
		};
	},
	reload,
	set,
	// Auto-login
	autoLoginEnabled: {
		get current() {
			return autoLoginEnabled;
		},
		set(enabled: boolean) {
			autoLoginEnabled = enabled;
		}
	}
};
