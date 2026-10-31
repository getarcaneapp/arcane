import { setMode } from 'mode-watcher';

import { timeFormatStore } from '#lib/stores/time-format.store.svelte.js';
import type { User } from '#lib/types/auth.js';
import { GLOBAL_SCOPE, SUDO_PERMISSION } from '#lib/types/auth.js';
import { setLocale } from '#lib/utils/formatting.js';
import {
	applyAccentColor,
	applyApplicationTheme,
	applyFontSize,
	applyGlassEffects,
	applyInterfaceAnimations,
	applyOledMode,
	FONT_SIZE_DEFAULT,
	setCustomThemes
} from '#lib/utils/theme.svelte.js';

let current = $state.raw<User | null>(null);

export const userHasPermissionInAnyEnvironment = (user: User | null | undefined, perm: string): boolean => {
	if (!user?.permissionsByEnv) return false;
	const global = user.permissionsByEnv[GLOBAL_SCOPE] ?? [];
	if (global.includes(SUDO_PERMISSION) || global.includes(perm)) return true;
	return Object.values(user.permissionsByEnv).some((permissions) => permissions.includes(perm));
};

const setUser = async (user: User) => {
	if (user.locale) {
		setLocale(user.locale, false);
	}
	applyFontSize(user.fontSize ?? FONT_SIZE_DEFAULT);
	timeFormatStore.set(user.timeFormat ?? 'auto');

	const preferences = user.preferences ?? {};
	setCustomThemes(preferences.customThemes ?? []);
	applyApplicationTheme(preferences.applicationTheme);
	applyAccentColor(preferences.accentColor ?? 'default');
	applyOledMode(preferences.oledMode ?? false);
	applyGlassEffects(preferences.glassEffectsEnabled ?? true);
	applyInterfaceAnimations(preferences.animationsEnabled ?? true);
	if (preferences.themeMode) {
		setMode(preferences.themeMode);
	}

	current = user;
};

const clearUser = () => {
	applyFontSize(FONT_SIZE_DEFAULT);
	timeFormatStore.reset();
	setCustomThemes([]);
	applyApplicationTheme('default');
	applyAccentColor('default');
	applyOledMode(false);
	applyGlassEffects(true);
	applyInterfaceAnimations(true);
	current = null;
};

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

const hasPermission = (perm: string, envId?: string): boolean => {
	const set = permissions(envId);
	if (set.has(SUDO_PERMISSION)) return true;
	return set.has(perm);
};

const hasAnyPermission = (perms: string[], envId?: string): boolean => {
	if (perms.length === 0) return true;
	const set = permissions(envId);
	if (set.has(SUDO_PERMISSION)) return true;
	return perms.some((p) => set.has(p));
};

const hasPermissionInAnyEnvironment = (perm: string): boolean => {
	return userHasPermissionInAnyEnvironment(current, perm);
};

const isGlobalAdmin = (): boolean => {
	const user = current;
	if (!user) return false;
	if (typeof user.isGlobalAdmin === 'boolean') return user.isGlobalAdmin;
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global?.includes(SUDO_PERMISSION)) return true;
	return false;
};

export default {
	get current() {
		return current;
	},
	setUser,
	clearUser,
	permissions,
	hasPermission,
	hasAnyPermission,
	hasPermissionInAnyEnvironment,
	isGlobalAdmin
};
