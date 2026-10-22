import { untrack } from 'svelte';

import { settingsService } from '#lib/services/settings-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { ResolvedSettings, SettingKey, Settings } from '#lib/types/settings.js';

let resolved = $state.raw<ResolvedSettings>();
const listeners = new Set<(settings: Settings | undefined) => void>();

const reload = async () => {
	const environmentId = await environmentStore.getCurrentEnvironmentId();
	const settings = await settingsService.getReadableSettings(environmentId);
	if (environmentId === environmentStore.selected?.id) set(settings);
};

const set = (settings: ResolvedSettings) => {
	resolved = settings;
	untrack(() => {
		for (const listener of listeners) listener(settings.values);
	});
};

const save = async (changes: Partial<Settings>) => {
	const environmentId = await environmentStore.getCurrentEnvironmentId();
	const base = resolved ?? (await settingsService.getReadableSettings(environmentId));
	const updated = await settingsService.updateSettingsForEnvironment(environmentId, base, changes);
	if (environmentId === environmentStore.selected?.id) set(updated);
	return updated;
};

const lock = (key: SettingKey) => {
	const meta = resolved?.meta[key];
	return meta?.locked ? meta : undefined;
};

let autoLoginEnabled = $state(false);

export default {
	get current() {
		return resolved?.values;
	},
	get resolved() {
		return resolved;
	},
	get runtime() {
		return resolved?.runtime ?? {};
	},
	get readOnly() {
		return resolved?.readOnly ?? false;
	},
	lock,
	isLocked(key: SettingKey) {
		return lock(key) !== undefined;
	},
	onChange(listener: (settings: Settings | undefined) => void) {
		listeners.add(listener);
		untrack(() => listener(resolved?.values));
		return () => {
			listeners.delete(listener);
		};
	},
	reload,
	set,
	save,
	autoLoginEnabled: {
		get current() {
			return autoLoginEnabled;
		},
		set(enabled: boolean) {
			autoLoginEnabled = enabled;
		}
	}
};
