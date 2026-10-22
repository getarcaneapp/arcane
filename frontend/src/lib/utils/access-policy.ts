import type { AccessSurface, PermissionsManifest, User } from '#lib/types/auth.js';
import { GLOBAL_SCOPE, SUDO_PERMISSION } from '#lib/types/auth.js';

type SurfaceIndex = Map<string, AccessSurface>;

let cachedSurfacesInternal: AccessSurface[] | undefined;
let cachedSurfaceIndexInternal: SurfaceIndex | undefined;

export function canReachAccessSurface(
	manifest: PermissionsManifest | null | undefined,
	surfaceId: string | undefined,
	user: User | null | undefined,
	envId?: string
): boolean {
	if (!surfaceId || !user) return false;
	const surfaces = manifest?.accessSurfaces;
	if (!surfaces?.length) return false;

	return canReachSurfaceInternal(getSurfaceIndexInternal(surfaces), surfaceId, user, envId, new Set());
}

export function getRouteAccessSurfaces(manifest: PermissionsManifest | null | undefined): AccessSurface[] {
	return (manifest?.accessSurfaces ?? [])
		.filter((surface) => !!surface.url)
		.slice()
		.sort((a, b) => {
			const aTemplates = (a.url?.match(/\{[^}]+\}/g) ?? []).length;
			const bTemplates = (b.url?.match(/\{[^}]+\}/g) ?? []).length;
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

	const pathSegments = splitPathInternal(path);
	const templateSegments = splitPathInternal(template);
	if (templateSegments.length !== pathSegments.length) return false;

	for (let i = 0; i < templateSegments.length; i++) {
		const expected = templateSegments[i];
		const actual = pathSegments[i];
		if (!expected || !actual) return false;
		if (isTemplateSegmentInternal(expected)) continue;
		if (expected !== actual) return false;
	}

	return true;
}

function canReachSurfaceInternal(
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
		const reachable = (surface.children ?? []).some((childId) =>
			canReachSurfaceInternal(surfaces, childId, user, envId, visiting)
		);
		visiting.delete(surfaceId);
		return reachable;
	}

	const permissions = surface.permissions ?? [];
	if (permissions.length === 0) return false;

	if (surface.matchMode === 'all-of') {
		return permissions.every((permission) => hasPermissionForScopeInternal(user, permission, surface.scopeMode, envId));
	}

	return permissions.some((permission) => hasPermissionForScopeInternal(user, permission, surface.scopeMode, envId));
}

function hasPermissionForScopeInternal(
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
			return permissionsForEnvInternal(user, envId).has(permission);
		case 'any-effective-scope':
			return Object.values(user.permissionsByEnv ?? {}).some((permissions) => permissions.includes(permission));
		default:
			return false;
	}
}

function permissionsForEnvInternal(user: User, envId: string | undefined): Set<string> {
	const out = new Set<string>();
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global) for (const permission of global) out.add(permission);
	if (envId && envId !== GLOBAL_SCOPE) {
		const scoped = user.permissionsByEnv?.[envId];
		if (scoped) for (const permission of scoped) out.add(permission);
	}
	return out;
}

function buildSurfaceIndexInternal(surfaces: AccessSurface[]): SurfaceIndex {
	return new Map(surfaces.map((surface) => [surface.id, surface]));
}

function getSurfaceIndexInternal(surfaces: AccessSurface[]): SurfaceIndex {
	if (cachedSurfacesInternal === surfaces && cachedSurfaceIndexInternal) {
		return cachedSurfaceIndexInternal;
	}

	cachedSurfacesInternal = surfaces;
	cachedSurfaceIndexInternal = buildSurfaceIndexInternal(surfaces);
	return cachedSurfaceIndexInternal;
}

function splitPathInternal(path: string): string[] {
	const withoutQuery = path.split('?')[0] ?? '';
	const withoutHash = withoutQuery.split('#')[0] ?? '';
	return withoutHash.split('/').filter(Boolean);
}

function isTemplateSegmentInternal(segment: string): boolean {
	return segment.startsWith(':') || (segment.startsWith('{') && segment.endsWith('}'));
}
