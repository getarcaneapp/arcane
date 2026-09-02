import userStore from '#lib/stores/user-store';
import { environmentStore } from '#lib/stores/environment.store.svelte';
import { m } from '#lib/paraglide/messages';
import { APIError } from '#lib/services/api-service';
import {
	canReachAccessSurface,
	getFallbackAccessSurfaces,
	getRouteAccessSurfaces,
	pathMatchesAccessSurface
} from '#lib/utils/access-policy';
import { GLOBAL_SCOPE, SUDO_PERMISSION } from '#lib/types/auth';
import type { PermissionsManifest, User } from '#lib/types/auth';

export function normalizeAuthenticationError(error: unknown, fallback: string): { kind: 'proxy' | 'other'; message: string } {
	if (error instanceof APIError) {
		const responseData: unknown = error.response?.data;
		const responseText = typeof responseData === 'string' ? responseData : '';
		const lowerResponse = responseText.toLowerCase();
		const isGatewayHTML =
			error.status === 502 &&
			(lowerResponse.includes('<html') || lowerResponse.includes('bad gateway') || lowerResponse.includes('openresty'));
		const isHeaderLimit =
			error.status === 431 ||
			(error.status === 400 &&
				(lowerResponse.includes('request header') || lowerResponse.includes('cookie')) &&
				(lowerResponse.includes('large') || lowerResponse.includes('too big')));

		if (isGatewayHTML || isHeaderLimit) {
			return { kind: 'proxy', message: m.auth_proxy_login_error() };
		}
		if (responseText.trimStart().startsWith('<')) {
			return { kind: 'other', message: fallback };
		}
	}

	return {
		kind: 'other',
		message: error instanceof Error ? error.message : fallback
	};
}

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
	'/account',
	'/dashboard',
	'/containers',
	'/customize',
	'/events',
	'/environments',
	'/images',
	'/projects',
	'/volumes',
	'/networks',
	'/ports',
	'/settings',
	'/swarm',
	'/updates'
];

const UNAUTHENTICATED_ONLY_PREFIXES = ['/login', '/oidc/login', '/img', '/favicon.ico'];
const AUTH_CALLBACK_PREFIXES = ['/oidc/callback', '/auth/oidc/callback'];

function isUserGlobalAdmin(user: User): boolean {
	if (typeof user.isGlobalAdmin === 'boolean') return user.isGlobalAdmin;
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global?.includes(SUDO_PERMISSION)) return true;
	return false;
}

export function userIsGlobalAdmin(user: User | null | undefined): boolean {
	return !!user && isUserGlobalAdmin(user);
}

export function userHasPermission(user: User | null | undefined, permission: string, envId?: string): boolean {
	if (!user?.permissionsByEnv) return false;
	const global = user.permissionsByEnv[GLOBAL_SCOPE] ?? [];
	if (global.includes(SUDO_PERMISSION) || global.includes(permission)) return true;
	return envId ? (user.permissionsByEnv[envId] ?? []).includes(permission) : false;
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
	accessManifestLoadFailed = false,
	landingPath: string = '/dashboard'
): string | null {
	const isSignedIn = !!user;

	if (path === '/') {
		return isSignedIn ? landingPath : '/login';
	}

	if (matchesAny(path, AUTH_CALLBACK_PREFIXES)) {
		return null;
	}

	if (!isSignedIn && matchesAny(path, PROTECTED_PREFIXES)) {
		return '/login';
	}

	if (isSignedIn && matchesAny(path, UNAUTHENTICATED_ONLY_PREFIXES)) {
		return landingPath;
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
