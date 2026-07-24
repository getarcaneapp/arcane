import type { User } from '#lib/types/auth';
import { GLOBAL_SCOPE, SUDO_PERMISSION } from '#lib/types/auth';
import { writable, get } from 'svelte/store';
import { setLocale } from '#lib/utils/formatting';
import {
	applyAccentColor,
	applyApplicationTheme,
	applyFontSize,
	applyGlassEffects,
	applyInterfaceAnimations,
	applyOledMode,
	FONT_SIZE_DEFAULT
} from '#lib/utils/theme';
import { setMode } from 'mode-watcher';
import { timeFormatStore } from '#lib/stores/time-format.store.svelte';

const userStore = writable<User | null>(null);

export const userHasPermissionInAnyEnvironment = (user: User | null | undefined, perm: string): boolean => {
	if (!user?.permissionsByEnv) return false;
	const global = user.permissionsByEnv[GLOBAL_SCOPE] ?? [];
	if (global.includes(SUDO_PERMISSION) || global.includes(perm)) return true;
	return Object.values(user.permissionsByEnv).some((permissions) => permissions.includes(perm));
};

const setUser = async (user: User) => {
	if (user.locale) {
		await setLocale(user.locale, false);
	}
	applyFontSize(user.fontSize ?? FONT_SIZE_DEFAULT);
	timeFormatStore.set(user.timeFormat ?? 'auto');

	const preferences = user.preferences ?? {};
	applyApplicationTheme(preferences.applicationTheme);
	applyAccentColor(preferences.accentColor ?? 'default');
	applyOledMode(preferences.oledMode ?? false);
	applyGlassEffects(preferences.glassEffectsEnabled ?? true);
	applyInterfaceAnimations(preferences.animationsEnabled ?? true);
	// Only override the device-local mode when the account carries an explicit
	// choice, so a browser that has never synced keeps its own light/dark mode.
	if (preferences.themeMode) {
		setMode(preferences.themeMode);
	}

	userStore.set(user);
};

const clearUser = () => {
	applyFontSize(FONT_SIZE_DEFAULT);
	timeFormatStore.reset();
	applyApplicationTheme('default');
	applyAccentColor('default');
	applyOledMode(false);
	applyGlassEffects(true);
	applyInterfaceAnimations(true);
	// Light/dark mode is deliberately left alone on logout: it is a device-local
	// preference and flipping it mid-session is jarring.
	userStore.set(null);
};

/**
 * Build the effective permission Set for the given environment. Includes
 * global permissions plus permissions scoped to `envId`.
 *
 * Pass `undefined` for `envId` to get only the global set (use this for
 * checking org-level permissions, or as a fallback before an env is selected).
 */
const permissions = (envId?: string): Set<string> => {
	const user = get(userStore);
	if (!user?.permissionsByEnv) return new Set();
	const out = new Set<string>();
	const global = user.permissionsByEnv[GLOBAL_SCOPE];
	if (global) for (const p of global) out.add(p);
	if (envId && envId !== GLOBAL_SCOPE) {
		const env = user.permissionsByEnv[envId];
		if (env) for (const p of env) out.add(p);
	}
	return out;
};

/** Returns true if the caller may perform `perm`. Sudo callers (`*` in global) always return true. */
const hasPermission = (perm: string, envId?: string): boolean => {
	const set = permissions(envId);
	if (set.has(SUDO_PERMISSION)) return true;
	return set.has(perm);
};

/** Returns true if the caller has ANY of the supplied permissions. */
const hasAnyPermission = (perms: string[], envId?: string): boolean => {
	if (perms.length === 0) return true;
	const set = permissions(envId);
	if (set.has(SUDO_PERMISSION)) return true;
	return perms.some((p) => set.has(p));
};

/** Returns true if the caller may perform `perm` in at least one effective environment scope. */
const hasPermissionInAnyEnvironment = (perm: string): boolean => {
	return userHasPermissionInAnyEnvironment(get(userStore), perm);
};

/** Returns true if the caller effectively holds global administrator access. */
const isGlobalAdmin = (): boolean => {
	const user = get(userStore);
	if (!user) return false;
	if (typeof user.isGlobalAdmin === 'boolean') return user.isGlobalAdmin;
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global?.includes(SUDO_PERMISSION)) return true;
	return false;
};

export default {
	subscribe: userStore.subscribe,
	setUser,
	clearUser,
	permissions,
	hasPermission,
	hasAnyPermission,
	hasPermissionInAnyEnvironment,
	isGlobalAdmin
};
