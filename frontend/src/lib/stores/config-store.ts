import { settingsService } from '$lib/services/settings-service';
import type { Settings } from '$lib/types/settings.type';
import { applyAccentColor } from '$lib/utils/accent-color-util';
import { bustLogoCache } from '$lib/utils/image.util';
import { get, writable } from 'svelte/store';

const settingsStore = writable<Settings>();

const reload = async () => {
	const previousSettings = get(settingsStore);
	const settings = await settingsService.getSettings();

	// Bust logo cache if accent color changed
	if (previousSettings && previousSettings.accentColor !== settings.accentColor) {
		bustLogoCache();
	}

	set(settings);
};

const set = (settings: Settings) => {
	applyAccentColor(settings.accentColor);
	settingsStore.set(settings);
};

// Auto-login state management
const AUTO_LOGIN_DISABLED_KEY = 'arcane_auto_login_disabled';
const autoLoginEnabledStore = writable<boolean>(false);

const setAutoLoginEnabled = (enabled: boolean) => {
	autoLoginEnabledStore.set(enabled);
};

const isAutoLoginEnabled = (): boolean => {
	return get(autoLoginEnabledStore);
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
	subscribe: settingsStore.subscribe,
	reload,
	set,
	// Auto-login
	autoLoginEnabled: {
		subscribe: autoLoginEnabledStore.subscribe,
		set: setAutoLoginEnabled,
		isEnabled: isAutoLoginEnabled,
		cacheDisabled: cacheAutoLoginDisabled,
		isKnownDisabled: isAutoLoginKnownDisabled,
		clearDisabledCache: clearAutoLoginDisabledCache
	}
};
