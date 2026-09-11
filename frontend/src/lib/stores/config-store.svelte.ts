import { settingsService } from '#lib/services/settings-service.js';
import type { Settings } from '#lib/types/settings.js';
import { untrack } from 'svelte';

let current = $state.raw<Settings>();
const listeners = new Set<(settings: Settings | undefined) => void>();

const reload = async () => {
	const settings = await settingsService.getSettings();

	set(settings);
};

const set = (settings: Settings) => {
	current = settings;
	untrack(() => {
		for (const listener of listeners) listener(settings);
	});
};

// Auto-login state management
const AUTO_LOGIN_DISABLED_KEY = 'arcane_auto_login_disabled';
let autoLoginEnabled = $state(false);

const setAutoLoginEnabled = (enabled: boolean) => {
	autoLoginEnabled = enabled;
};

const cacheAutoLoginDisabled = (): void => {
	if (typeof sessionStorage !== 'undefined') {
		sessionStorage.setItem(AUTO_LOGIN_DISABLED_KEY, 'true');
	}
};

const isAutoLoginKnownDisabled = (): boolean => {
	if (typeof sessionStorage === 'undefined') return false;
	return sessionStorage.getItem(AUTO_LOGIN_DISABLED_KEY) === 'true';
};

const clearAutoLoginDisabledCache = (): void => {
	if (typeof sessionStorage !== 'undefined') {
		sessionStorage.removeItem(AUTO_LOGIN_DISABLED_KEY);
	}
};

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
		set: setAutoLoginEnabled,
		cacheDisabled: cacheAutoLoginDisabled,
		isKnownDisabled: isAutoLoginKnownDisabled,
		clearDisabledCache: clearAutoLoginDisabledCache
	}
};
