import type { AccessSurface, PermissionsManifest } from '#lib/types/role.js';
import { GLOBAL_SCOPE, SUDO_PERMISSION } from '#lib/types/role.js';
import type { User } from '#lib/types/user.js';

type SurfaceIndex = Map<string, AccessSurface>;

let cachedSurfaces: AccessSurface[] | undefined;
let cachedSurfaceIndex: SurfaceIndex | undefined;

export function canReachAccessSurface(
	manifest: PermissionsManifest | null | undefined,
	surfaceId: string | undefined,
	user: User | null | undefined,
	envId?: string
): boolean {
	if (!surfaceId || !user) return false;
	const surfaces = manifest?.accessSurfaces;
	if (!surfaces?.length) return false;

	return canReachSurface(getSurfaceIndex(surfaces), surfaceId, user, envId, new Set());
}

export function getRouteAccessSurfaces(manifest: PermissionsManifest | null | undefined): AccessSurface[] {
	return (manifest?.accessSurfaces ?? [])
		.filter((surface) => !!surface.url)
		.slice()
		.sort((a, b) => {
			const aTemplates = (a.url ?? '').split('{').length - 1;
			const bTemplates = (b.url ?? '').split('{').length - 1;
			if (aTemplates !== bTemplates) return aTemplates - bTemplates;
			return (b.url?.length ?? 0) - (a.url?.length ?? 0);
		});
}

export function getFallbackAccessSurfaces(manifest: PermissionsManifest | null | undefined): AccessSurface[] {
	return (manifest?.accessSurfaces ?? [])
		.filter((surface) => !!surface.url && !!surface.fallbackOrder)
		.slice()
		.sort((a, b) => (a.fallbackOrder ?? 0) - (b.fallbackOrder ?? 0));
}

export function pathMatchesAccessSurface(path: string, surface: AccessSurface): boolean {
	const template = surface.url;
	if (!template) return false;

	const pathSegments = splitPath(path);
	const templateSegments = splitPath(template);
	if (templateSegments.length !== pathSegments.length) return false;

	for (let i = 0; i < templateSegments.length; i++) {
		const expected = templateSegments[i];
		const actual = pathSegments[i];
		if (!expected || !actual) return false;
		if (isTemplateSegment(expected)) continue;
		if (expected !== actual) return false;
	}

	return true;
}

function canReachSurface(
	surfaces: SurfaceIndex,
	surfaceId: string,
	user: User,
	envId: string | undefined,
	visiting: Set<string>
): boolean {
	if (visiting.has(surfaceId)) return false;

	const surface = surfaces.get(surfaceId);
	if (!surface) return false;

	if (surface.accessMode === 'any-child') {
		visiting.add(surfaceId);
		const reachable = (surface.children ?? []).some((childId) => canReachSurface(surfaces, childId, user, envId, visiting));
		visiting.delete(surfaceId);
		return reachable;
	}

	const permissions = surface.permissions ?? [];
	if (permissions.length === 0) return false;

	if (surface.matchMode === 'all-of') {
		return permissions.every((permission) => hasPermissionForScope(user, permission, surface.scopeMode, envId));
	}

	return permissions.some((permission) => hasPermissionForScope(user, permission, surface.scopeMode, envId));
}

function hasPermissionForScope(
	user: User,
	permission: string,
	scopeMode: AccessSurface['scopeMode'],
	envId: string | undefined
): boolean {
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE] ?? [];
	if (global.includes(SUDO_PERMISSION)) return true;

	switch (scopeMode) {
		case 'global-only':
			return global.includes(permission);
		case 'env-plus-global':
			return permissionsForEnv(user, envId).has(permission);
		case 'any-effective-scope':
			return Object.values(user.permissionsByEnv ?? {}).some((permissions) => permissions.includes(permission));
		default:
			return false;
	}
}

function permissionsForEnv(user: User, envId: string | undefined): Set<string> {
	const out = new Set<string>();
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global) for (const permission of global) out.add(permission);
	if (envId && envId !== GLOBAL_SCOPE) {
		const scoped = user.permissionsByEnv?.[envId];
		if (scoped) for (const permission of scoped) out.add(permission);
	}
	return out;
}

function buildSurfaceIndex(surfaces: AccessSurface[]): SurfaceIndex {
	return new Map(surfaces.map((surface) => [surface.id, surface]));
}

function getSurfaceIndex(surfaces: AccessSurface[]): SurfaceIndex {
	if (cachedSurfaces === surfaces && cachedSurfaceIndex) {
		return cachedSurfaceIndex;
	}

	cachedSurfaces = surfaces;
	cachedSurfaceIndex = buildSurfaceIndex(surfaces);
	return cachedSurfaceIndex;
}

function splitPath(path: string): string[] {
	const withoutQuery = path.split('?')[0] ?? '';
	const withoutHash = withoutQuery.split('#')[0] ?? '';
	return withoutHash.split('/').filter(Boolean);
}

function isTemplateSegment(segment: string): boolean {
	return segment.startsWith(':') || (segment.startsWith('{') && segment.endsWith('}'));
}
