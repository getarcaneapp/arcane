import { get } from 'svelte/store';
import userStore from '$lib/stores/user-store';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { getRouteAccessRules, getRouteFallbackRules, type RouteAccessRule } from '$lib/config/navigation-config';
import { BUILT_IN_ROLE_ADMIN, GLOBAL_SCOPE, SUDO_PERMISSION } from '$lib/types/auth';
import type { User } from '$lib/types/auth';

// --- Store-backed permission checks (for .svelte / runtime) ---

function resolveEnvId(envId?: string): string | undefined {
	if (envId) return envId;
	const selected = environmentStore.selected;
	if (!selected?.id) return undefined;
	return selected.id;
}

export function hasPermission(perm: string, envId?: string): boolean {
	return userStore.hasPermission(perm, resolveEnvId(envId));
}

export function hasAnyPermission(perms: string[], envId?: string): boolean {
	return userStore.hasAnyPermission(perms, resolveEnvId(envId));
}

export function permissions(envId?: string): Set<string> {
	return userStore.permissions(resolveEnvId(envId));
}

export function isGlobalAdmin(): boolean {
	return userStore.isGlobalAdmin();
}

export function hasAnyAccess(user: User | null): boolean {
	if (!user?.permissionsByEnv) return false;
	for (const perms of Object.values(user.permissionsByEnv)) {
		if (perms.length > 0) return true;
	}
	return false;
}

export { GLOBAL_SCOPE };
export { get };

// --- Load-function helpers (run before stores hydrate) ---

const PROTECTED_PREFIXES = [
	'/dashboard',
	'/compose',
	'/containers',
	'/customize',
	'/events',
	'/environments',
	'/images',
	'/volumes',
	'/networks',
	'/ports',
	'/settings',
	'/swarm',
	'/updates'
];

const UNAUTHENTICATED_ONLY_PREFIXES = ['/login', '/oidc/login', '/oidc/callback', '/auth/oidc/callback', '/img', '/favicon.ico'];

function isUserGlobalAdmin(user: User): boolean {
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global?.includes(SUDO_PERMISSION)) return true;
	return !!user.roleAssignments?.some((a) => a.roleId === BUILT_IN_ROLE_ADMIN && !a.environmentId);
}

export function userIsGlobalAdmin(user: User | null | undefined): boolean {
	return !!user && isUserGlobalAdmin(user);
}

function isAdminOnlyRoute(path: string): boolean {
	return path === '/settings/roles/new' || /^\/settings\/roles\/[^/]+/.test(path);
}

const matchesAny = (path: string, prefixes: string[]) =>
	prefixes.some((prefix) => path === prefix || path.startsWith(`${prefix}/`));

function permissionsForEnv(user: User, envId?: string): Set<string> {
	const out = new Set<string>();
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global) for (const p of global) out.add(p);
	if (envId && envId !== GLOBAL_SCOPE) {
		const env = user.permissionsByEnv?.[envId];
		if (env) for (const p of env) out.add(p);
	}
	return out;
}

function userCanReach(user: User, perms: string[], scope: 'global' | 'env', envId?: string): boolean {
	const set = permissionsForEnv(user, scope === 'env' ? envId : undefined);
	if (set.has(SUDO_PERMISSION)) return true;
	return perms.some((p) => set.has(p));
}

function userHasAnyAccess(user: User): boolean {
	if (!user.permissionsByEnv) return false;
	for (const perms of Object.values(user.permissionsByEnv)) {
		if (perms.length > 0) return true;
	}
	return false;
}

function pickFallbackRoute(user: User, envId?: string): string {
	for (const rule of getRouteFallbackRules()) {
		if (userCanReach(user, rule.perms, rule.scope, envId)) {
			return rule.prefix;
		}
	}
	return '/no-access';
}

export function getAuthRedirectPath(path: string, user: User | null, envId?: string): string | null {
	const isSignedIn = !!user;

	if (path === '/') {
		return isSignedIn ? '/dashboard' : '/login';
	}

	if (!isSignedIn && matchesAny(path, PROTECTED_PREFIXES)) {
		return '/login';
	}

	if (isSignedIn && matchesAny(path, UNAUTHENTICATED_ONLY_PREFIXES)) {
		return '/dashboard';
	}

	if (!isSignedIn || !user) return null;

	if (path !== '/no-access' && !userHasAnyAccess(user)) {
		return '/no-access';
	}

	if (isAdminOnlyRoute(path) && !isUserGlobalAdmin(user)) {
		return '/settings/roles';
	}

	const sorted: RouteAccessRule[] = [...getRouteAccessRules()].sort((a, b) => b.prefix.length - a.prefix.length);
	for (const rule of sorted) {
		if (matchesAny(path, [rule.prefix])) {
			if (!userCanReach(user, rule.perms, rule.scope, envId)) {
				const fallback = pickFallbackRoute(user, envId);
				return fallback === path ? '/no-access' : fallback;
			}
			break;
		}
	}

	return null;
}
