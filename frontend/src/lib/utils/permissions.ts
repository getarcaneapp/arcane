import type { PermissionsManifest } from '#lib/types/auth.js';

function requiresByPermission(manifest: PermissionsManifest): Map<string, string[]> {
	return new Map(
		manifest.resources.flatMap((resource) =>
			resource.actions.map((action) => [action.permission, action.requires ?? []] as const)
		)
	);
}

export function normalizePermissionSelection(manifest: PermissionsManifest, selected: string[]): string[] {
	const requires = requiresByPermission(manifest);
	return [...new Set(selected.flatMap((permission) => [permission, ...(requires.get(permission) ?? [])]))];
}

export function removePermissionSelection(manifest: PermissionsManifest, selected: string[], removed: string[]): string[] {
	const requires = requiresByPermission(manifest);
	const removedSet = new Set(removed);
	return selected.filter(
		(permission) => !removedSet.has(permission) && !(requires.get(permission) ?? []).some((required) => removedSet.has(required))
	);
}
