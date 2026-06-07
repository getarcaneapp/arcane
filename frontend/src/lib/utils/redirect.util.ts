import type { User } from '$lib/types/user.type';

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
	'/settings'
];

const UNAUTHENTICATED_ONLY_PREFIXES = ['/login', '/oidc/login', '/oidc/callback', '/auth/oidc/callback', '/img', '/favicon.ico'];

const ADMIN_ONLY_PREFIXES = ['/settings', '/events', '/customize/registries', '/customize/variables'];

/**
 * Checks if a path matches a prefix exactly or as a parent directory
 */
const matchesAny = (path: string, prefixes: string[]) =>
	prefixes.some((prefix) => path === prefix || path.startsWith(`${prefix}/`));

export function getAuthRedirectPath(path: string, user: User | null) {
	const isSignedIn = !!user;
	const isAdmin = user?.roles.includes('admin');

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

	// 4. Redirect non-admins away from restricted management areas
	if (isSignedIn && !isAdmin && matchesAny(path, ADMIN_ONLY_PREFIXES)) {
		return '/dashboard';
	}

	return null;
}
