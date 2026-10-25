import { goto as kitGoto } from '$app/navigation';
import { resolve } from '$app/paths';

/**
 * The base path the app is served under, empty at the root of a host. SvelteKit
 * reads it at runtime from the page the backend serves (index.html is rewritten
 * for the path of APP_URL), so one build works at the root or under any sub-path.
 */
export const base: string = resolve('/').replace(/\/$/, '');

/**
 * Prefix a root-relative URL (`/api/...`, `/containers`, ...) with the
 * configured base path. URLs that are absolute (`https://...`), protocol
 * relative (`//...`) or already carry the prefix are returned unchanged, so
 * this function is safe to apply more than once.
 */
export function withBase(url: string): string {
	if (!base) return url;
	if (!url.startsWith('/') || url.startsWith('//')) return url;
	if (url === base || url.startsWith(`${base}/`)) return url;
	return `${base}${url}`;
}

/**
 * Remove the configured base path prefix from a browser location path so it
 * can be compared against route paths configured without the base path.
 */
export function stripBase(path: string): string {
	if (!base) return path;
	if (path === base) return '/';
	if (path.startsWith(`${base}/`)) return path.slice(base.length);
	return path;
}

/**
 * SvelteKit's `goto`, with root-relative paths resolved against the base path.
 * Use this instead of importing `goto` from `$app/navigation` directly.
 */
export function goto(...[url, opts]: Parameters<typeof kitGoto>): ReturnType<typeof kitGoto> {
	return kitGoto(typeof url === 'string' ? withBase(url) : url, opts);
}
