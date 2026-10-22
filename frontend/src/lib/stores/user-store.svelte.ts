import { setMode } from 'mode-watcher';
import { untrack } from 'svelte';

import { PERMISSIONS, type Permission } from '#lib/permissions.generated.js';
import { timeFormatStore } from '#lib/stores/time-format.store.svelte.js';
import type { User } from '#lib/types/auth.js';
import { GLOBAL_SCOPE } from '#lib/types/auth.js';
import { setLocale } from '#lib/utils/formatting.js';
import {
	applyAccentColor,
	applyApplicationTheme,
	applyFontSize,
	applyGlassEffects,
	applyInterfaceAnimations,
	applyOledMode,
	FONT_SIZE_DEFAULT
} from '#lib/utils/theme.svelte.js';

let current = $state.raw<User | null>(null);
const listeners = new Set<(user: User | null) => void>();

export const userHasPermissionInAnyEnvironment = (user: User | null | undefined, perm: Permission): boolean => {
	if (!user?.permissionsByEnv) return false;
	const global = user.permissionsByEnv[GLOBAL_SCOPE] ?? [];
	if (global.includes(PERMISSIONS.ALL) || global.includes(perm)) return true;
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

	current = user;
	untrack(() => {
		for (const listener of listeners) listener(user);
	});
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
	current = null;
	untrack(() => {
		for (const listener of listeners) listener(null);
	});
};

/**
 * Build the effective permission Set for the given environment. Includes
 * global permissions plus permissions scoped to `envId`.
 *
 * Pass `undefined` for `envId` to get only the global set (use this for
 * checking org-level permissions, or as a fallback before an env is selected).
 */
const permissions = (envId?: string): Set<string> => {
	const user = current;
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
const hasPermission = (perm: Permission, envId?: string): boolean => {
	const set = permissions(envId);
	return set.has(PERMISSIONS.ALL) || set.has(perm);
};

/** Returns true if the caller has ANY of the supplied permissions. */
const hasAnyPermission = (perms: Permission[], envId?: string): boolean => {
	if (perms.length === 0) return true;
	const set = permissions(envId);
	if (set.has(PERMISSIONS.ALL)) return true;
	return perms.some((p) => set.has(p));
};

/** Returns true if the caller may perform `perm` in at least one effective environment scope. */
const hasPermissionInAnyEnvironment = (perm: Permission): boolean => {
	return userHasPermissionInAnyEnvironment(current, perm);
};

const isScoped = (perm: Permission, envId?: string): boolean => {
	const user = current;
	if (!user || !hasPermission(perm, envId)) return false;
	if (user.isGlobalAdmin) return false;
	const unrestricted = (scope: string) =>
		(user.permissionsByEnv?.[scope] ?? []).includes(perm) && !(user.scopedPermissionsByEnv?.[scope] ?? []).includes(perm);
	return !unrestricted(GLOBAL_SCOPE) && !(envId && envId !== GLOBAL_SCOPE && unrestricted(envId));
};

/** Returns true if the caller effectively holds global administrator access. */
const isGlobalAdmin = (): boolean => current?.isGlobalAdmin === true;

export default {
	get current() {
		return current;
	},
	onChange(listener: (user: User | null) => void) {
		listeners.add(listener);
		untrack(() => listener(current));
		return () => {
			listeners.delete(listener);
		};
	},
	setUser,
	clearUser,
	permissions,
	hasPermission,
	hasAnyPermission,
	hasPermissionInAnyEnvironment,
	isScoped,
	isGlobalAdmin
};
