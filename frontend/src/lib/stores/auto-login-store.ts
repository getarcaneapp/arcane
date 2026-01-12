import { writable, get } from 'svelte/store';

const AUTO_LOGIN_DISABLED_KEY = 'arcane_auto_login_disabled';

const autoLoginEnabledStore = writable<boolean>(false);

/**
 * Set the auto-login enabled state
 */
const setEnabled = (enabled: boolean) => {
	autoLoginEnabledStore.set(enabled);
};

/**
 * Check if auto-login is enabled
 */
const isEnabled = (): boolean => {
	return get(autoLoginEnabledStore);
};

/**
 * Cache that auto-login is disabled (persists in sessionStorage)
 */
const cacheDisabled = (): void => {
	if (typeof sessionStorage !== 'undefined') {
		sessionStorage.setItem(AUTO_LOGIN_DISABLED_KEY, 'true');
	}
};

/**
 * Check if auto-login is known to be disabled (from sessionStorage cache)
 */
const isKnownDisabled = (): boolean => {
	if (typeof sessionStorage === 'undefined') return false;
	return sessionStorage.getItem(AUTO_LOGIN_DISABLED_KEY) === 'true';
};

/**
 * Clear the disabled cache (e.g., on logout)
 */
const clearDisabledCache = (): void => {
	if (typeof sessionStorage !== 'undefined') {
		sessionStorage.removeItem(AUTO_LOGIN_DISABLED_KEY);
	}
};

export const autoLoginStore = {
	subscribe: autoLoginEnabledStore.subscribe,
	setEnabled,
	isEnabled,
	cacheDisabled,
	isKnownDisabled,
	clearDisabledCache
};

