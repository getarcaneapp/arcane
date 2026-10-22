import { m } from '#lib/paraglide/messages.js';
import { PERMISSIONS, type Permission } from '#lib/permissions.generated.js';
import { APIError } from '#lib/services/api-service.js';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { GLOBAL_SCOPE } from '#lib/types/auth.js';
import type { PermissionsManifest, User } from '#lib/types/auth.js';
import {
	canReachAccessSurface,
	getFallbackAccessSurfaces,
	getRouteAccessSurfaces,
	pathMatchesAccessSurface
} from '#lib/utils/access-policy.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';

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

export function hasPermission(perm: Permission, envId?: string): boolean {
	return userStore.hasPermission(perm, resolveEnvId(envId));
}

export function hasAnyPermission(perms: Permission[], envId?: string): boolean {
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
	'/podman',
	'/ports',
	'/settings',
	'/swarm',
	'/updates'
];

const UNAUTHENTICATED_ONLY_PREFIXES = ['/login', '/oidc/login', '/img', '/favicon.ico'];
const AUTH_CALLBACK_PREFIXES = ['/oidc/callback', '/auth/oidc/callback'];

export function userIsGlobalAdmin(user: User | null | undefined): boolean {
	return user?.isGlobalAdmin === true;
}

export function userHasPermission(user: User | null | undefined, permission: Permission, envId?: string): boolean {
	if (!user?.permissionsByEnv) return false;
	const global = user.permissionsByEnv[GLOBAL_SCOPE] ?? [];
	if (global.includes(PERMISSIONS.ALL) || global.includes(permission)) return true;
	return envId ? (user.permissionsByEnv[envId] ?? []).includes(permission) : false;
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

/** Whether a surface is reachable in `envId` or, while the all environments preview is on, in any shown environment. */
export function canReachShownAccessSurface(
	accessManifest: PermissionsManifest | null | undefined,
	surfaceId: string | undefined,
	user: User,
	envId: string | undefined
): boolean {
	const envIds = environmentStore.selected ? [envId, ...environmentStore.shown.map((env) => env.id)] : [envId];
	return envIds.some((id) => canReachAccessSurface(accessManifest, surfaceId, user, id));
}

/**
 * The first shown environment that can open `path` when `envId` cannot. Pages that show one environment
 * at a time load the selected one, so the preview switches to this one instead of failing.
 */
export function shownEnvironmentForRoute(
	path: string,
	user: User,
	envId: string | undefined,
	accessManifest: PermissionsManifest | null | undefined
): EnvironmentRef | undefined {
	const surface = getRouteAccessSurfaces(accessManifest).find((candidate) => pathMatchesAccessSurface(path, candidate));
	if (!surface || canReachAccessSurface(accessManifest, surface.id, user, envId)) return undefined;
	const reachable = environmentStore.shown.filter((env) => canReachAccessSurface(accessManifest, surface.id, user, env.id));
	// An online environment is preferred, so the page can load; local is always reachable.
	const online = reachable.find((env) => {
		const environment = environmentStore.available.find((candidate) => candidate.id === env.id);
		return env.id === LOCAL_DOCKER_ENVIRONMENT_ID || (!!environment && isEnvironmentOnline(environment));
	});
	return online ?? reachable[0];
}

function pickFallbackRoute(
	user: User,
	envId: string | undefined,
	accessManifest: PermissionsManifest | null | undefined
): string {
	for (const surface of getFallbackAccessSurfaces(accessManifest)) {
		if (canReachShownAccessSurface(accessManifest, surface.id, user, envId)) {
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
	if (path === '/') {
		return user ? landingPath : '/login';
	}

	if (matchesAny(path, AUTH_CALLBACK_PREFIXES)) {
		return null;
	}

	if (!user) return matchesAny(path, PROTECTED_PREFIXES) ? '/login' : null;
	if (matchesAny(path, UNAUTHENTICATED_ONLY_PREFIXES)) return landingPath;
	if (
		!accessManifestLoadFailed &&
		path !== '/no-access' &&
		!accessManifest?.accessSurfaces?.length &&
		matchesAny(path, PROTECTED_PREFIXES)
	) {
		return '/no-access';
	}

	if (path !== '/no-access' && !userHasAnyAccess(user)) {
		return '/no-access';
	}

	const surface = getRouteAccessSurfaces(accessManifest).find((candidate) => pathMatchesAccessSurface(path, candidate));
	if (surface && !canReachShownAccessSurface(accessManifest, surface.id, user, envId)) {
		const fallback = pickFallbackRoute(user, envId, accessManifest);
		return fallback === path ? '/no-access' : fallback;
	}

	return null;
}
