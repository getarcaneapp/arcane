import { goto as kitGoto } from '$app/navigation';
import { resolve } from '$app/paths';

export const base: string = resolve('/').replace(/\/$/, '');

export function withBase(url: string): string {
	if (!base || !url.startsWith('/') || url.startsWith('//') || hasBase(url)) return url;
	return `${base}${url}`;
}

export function stripBase(path: string): string {
	if (!base || !hasBase(path)) return path;
	const rest = path.slice(base.length);
	return rest.startsWith('/') ? rest : `/${rest}`;
}

function hasBase(url: string): boolean {
	if (!url.startsWith(base)) return false;
	const next = url.charAt(base.length);
	return next === '' || next === '/' || next === '?' || next === '#';
}

export function goto(...[url, opts]: Parameters<typeof kitGoto>): ReturnType<typeof kitGoto> {
	return kitGoto(typeof url === 'string' ? withBase(url) : url, opts);
}
