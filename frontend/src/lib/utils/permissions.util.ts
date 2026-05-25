import userStore from '$lib/stores/user-store';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { GLOBAL_SCOPE } from '$lib/types/role.type';
import { get } from 'svelte/store';
import type { User } from '$lib/types/user.type';

/**
 * Resolve the env ID to use for an RBAC check. When the caller doesn't pass
 * one, fall back to the currently-selected environment from the store.
 * Returns `undefined` when no env is selected — callers should treat that
 * as "global-only check" (env-scoped permissions will deny).
 */
function resolveEnvId(envId?: string): string | undefined {
	if (envId) return envId;
	const selected = environmentStore.selected;
	if (!selected?.id) return undefined;
	return selected.id;
}

/**
 * Check whether the current user holds `perm` on the given environment.
 *
 * Reactive callers (inside `.svelte` files) should wrap this in `$derived`
 * along with a read of `environmentStore.selected?.id` so the value re-computes
 * when the env switches.
 */
export function hasPermission(perm: string, envId?: string): boolean {
	return userStore.hasPermission(perm, resolveEnvId(envId));
}

/** Returns true if the user has ANY of the supplied permissions on `envId`. */
export function hasAnyPermission(perms: string[], envId?: string): boolean {
	return userStore.hasAnyPermission(perms, resolveEnvId(envId));
}

/** Returns the full effective permission set for the given env (global ∪ env). */
export function permissions(envId?: string): Set<string> {
	return userStore.permissions(resolveEnvId(envId));
}

/** Returns true if the caller is a global admin (or sudo). */
export function isGlobalAdmin(): boolean {
	return userStore.isGlobalAdmin();
}

/**
 * Returns true if the user holds AT LEAST one permission on any environment
 * (or globally). Useful for the "no access at all" fallback redirect.
 */
export function hasAnyAccess(user: User | null): boolean {
	if (!user?.permissionsByEnv) return false;
	for (const perms of Object.values(user.permissionsByEnv)) {
		if (perms.length > 0) return true;
	}
	return false;
}

// Keep imports tree-shake-friendly: re-export only what callers need.
export { GLOBAL_SCOPE };

// `get` is imported above to satisfy the (now-removed) hasAnyAccess fallback;
// kept exported so callers can grab the raw store snapshot if needed.
export { get };
