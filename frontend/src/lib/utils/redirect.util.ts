import type { User } from '$lib/types/user.type';
import { GLOBAL_SCOPE, SUDO_PERMISSION, BUILT_IN_ROLE_ADMIN } from '$lib/types/role.type';
import { getRouteAccessRules, getRouteFallbackRules, type RouteAccessRule } from '$lib/config/navigation-config';

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

function isGlobalAdmin(user: User): boolean {
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global?.includes(SUDO_PERMISSION)) return true;
	return !!user.roleAssignments?.some((a) => a.roleId === BUILT_IN_ROLE_ADMIN && !a.environmentId);
}

/**
 * Exported global-admin check for use in `+page.ts` load functions, where the
 * user is available via `await parent()` but the store-backed helper in
 * `permissions.util.ts` is not appropriate (load runs before stores hydrate).
 */
export function userIsGlobalAdmin(user: User | null | undefined): boolean {
	return !!user && isGlobalAdmin(user);
}

/**
 * True for routes reserved for global admins. Role creation/editing and OIDC
 * mapping management are intentionally not delegatable to non-admin users —
 * the matching backend routes are gated by RequireGlobalAdmin. Note that the
 * bare `/settings/roles` list is still reachable for readers; only the
 * `/new` and `/<id>` subroutes (the editor) are admin-only.
 */
function isAdminOnlyRoute(path: string): boolean {
	return path === '/settings/roles/new' || /^\/settings\/roles\/[^/]+/.test(path);
}

/** Checks if a path matches a prefix exactly or as a parent directory. */
const matchesAny = (path: string, prefixes: string[]) =>
	prefixes.some((prefix) => path === prefix || path.startsWith(`${prefix}/`));

/** Returns the permission set the user effectively holds at `envId` (global ∪ env). */
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

/** Returns true if the user can satisfy any of `perms` at the given env scope. */
function userCanReach(user: User, perms: string[], scope: 'global' | 'env', envId?: string): boolean {
	const set = permissionsForEnv(user, scope === 'env' ? envId : undefined);
	if (set.has(SUDO_PERMISSION)) return true;
	return perms.some((p) => set.has(p));
}

/** Returns true if the user holds any permission anywhere. */
function hasAnyAccess(user: User): boolean {
	if (!user.permissionsByEnv) return false;
	for (const perms of Object.values(user.permissionsByEnv)) {
		if (perms.length > 0) return true;
	}
	return false;
}

/**
 * Pick a fallback route the user CAN reach. Walks the gated routes in a
 * sensible priority order (dashboard first, then resources, then settings)
 * and returns the first one the user satisfies. Returns `/no-access` if
 * nothing matches — that page is always reachable.
 */
function pickFallbackRoute(user: User, envId?: string): string {
	for (const rule of getRouteFallbackRules()) {
		if (userCanReach(user, rule.perms, rule.scope, envId)) {
			return rule.prefix;
		}
	}
	return '/no-access';
}

/**
 * getAuthRedirectPath decides where to send the caller based on the current
 * path, authentication state, and effective permissions. Returns null when
 * no redirect is needed.
 *
 * @param envId the currently-selected environment ID; used for env-scoped
 * permission checks. Pass undefined if no env is selected.
 */
export function getAuthRedirectPath(path: string, user: User | null, envId?: string): string | null {
	const isSignedIn = !!user;

	// 1. Handle root path
	if (path === '/') {
		return isSignedIn ? '/dashboard' : '/login';
	}

	// 2. Redirect unauthenticated users away from protected areas
	if (!isSignedIn && matchesAny(path, PROTECTED_PREFIXES)) {
		return '/login';
	}

	// 3. Redirect signed-in users away from login/auth pages
	if (isSignedIn && matchesAny(path, UNAUTHENTICATED_ONLY_PREFIXES)) {
		return '/dashboard';
	}

	if (!isSignedIn || !user) return null;

	// 4. Users with zero permissions land on /no-access (unless already there).
	if (path !== '/no-access' && !hasAnyAccess(user)) {
		return '/no-access';
	}

	// 5. Admin-only routes (role editor) bounce non-admins back to the roles
	// list — they can still read role definitions, just not edit them.
	if (isAdminOnlyRoute(path) && !isGlobalAdmin(user)) {
		return '/settings/roles';
	}

	// 6. Per-route permission gating. Walk the most-specific prefix first.
	const sorted: RouteAccessRule[] = [...getRouteAccessRules()].sort((a, b) => b.prefix.length - a.prefix.length);
	for (const rule of sorted) {
		if (matchesAny(path, [rule.prefix])) {
			if (!userCanReach(user, rule.perms, rule.scope, envId)) {
				// Don't fall back to a route the user also can't reach — that
				// produces an effective loop where every redirect target bounces
				// to the same place. Pick the first reachable route in priority
				// order, or /no-access if nothing fits.
				const fallback = pickFallbackRoute(user, envId);
				return fallback === path ? '/no-access' : fallback;
			}
			break;
		}
	}

	return null;
}
