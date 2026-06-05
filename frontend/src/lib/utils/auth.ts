import userStore from '$lib/stores/user-store';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import {
	canReachAccessSurface,
	getFallbackAccessSurfaces,
	getRouteAccessSurfaces,
	pathMatchesAccessSurface
} from '$lib/utils/access-policy';
import { GLOBAL_SCOPE, SUDO_PERMISSION } from '$lib/types/auth';
import type { PermissionsManifest, User } from '$lib/types/auth';

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

export function isGlobalAdmin(): boolean {
	return userStore.isGlobalAdmin();
}

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
	if (typeof user.isGlobalAdmin === 'boolean') return user.isGlobalAdmin;
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global?.includes(SUDO_PERMISSION)) return true;
	return false;
}

export function userIsGlobalAdmin(user: User | null | undefined): boolean {
	return !!user && isUserGlobalAdmin(user);
}

function isAdminOnlyRoute(path: string): boolean {
	return path === '/settings/roles/new' || /^\/settings\/roles\/[^/]+/.test(path);
}

const matchesAny = (path: string, prefixes: string[]) =>
	prefixes.some((prefix) => path === prefix || path.startsWith(`${prefix}/`));

function userHasAnyAccess(user: User): boolean {
	if (!user.permissionsByEnv) return false;
	for (const perms of Object.values(user.permissionsByEnv)) {
		if (perms.length > 0) return true;
	}
	return false;
}

function pickFallbackRoute(
	user: User,
	envId: string | undefined,
	accessManifest: PermissionsManifest | null | undefined
): string {
	for (const surface of getFallbackAccessSurfaces(accessManifest)) {
		if (canReachAccessSurface(accessManifest, surface.id, user, envId)) {
			return surface.url ?? '/no-access';
		}
	}
	return '/no-access';
}

export function getAuthRedirectPath(
	path: string,
	user: User | null,
	envId?: string,
	accessManifest?: PermissionsManifest | null,
	accessManifestLoadFailed = false
): string | null {
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

	if (
		isSignedIn &&
		!accessManifestLoadFailed &&
		path !== '/no-access' &&
		!accessManifest?.accessSurfaces?.length &&
		matchesAny(path, PROTECTED_PREFIXES)
	) {
		return '/no-access';
	}

	if (!isSignedIn || !user) return null;

	if (path !== '/no-access' && !userHasAnyAccess(user)) {
		return '/no-access';
	}

	if (isAdminOnlyRoute(path) && !isUserGlobalAdmin(user)) {
		return '/settings/roles';
	}

	for (const surface of getRouteAccessSurfaces(accessManifest)) {
		if (pathMatchesAccessSurface(path, surface)) {
			if (!canReachAccessSurface(accessManifest, surface.id, user, envId)) {
				const fallback = pickFallbackRoute(user, envId, accessManifest);
				return fallback === path ? '/no-access' : fallback;
			}
			break;
		}
	}

	return null;
}
