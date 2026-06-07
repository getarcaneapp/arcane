import type { PermissionsManifest } from '$lib/types/auth';

export function normalizePermissionSelection(manifest: PermissionsManifest, selected: string[]): string[] {
	const requiredByPermission = new Map<string, string[]>();
	for (const resource of manifest.resources) {
		for (const action of resource.actions) {
			requiredByPermission.set(action.permission, action.requires ?? []);
		}
	}

	const out = new Set(selected);
	let changed = true;
	while (changed) {
		changed = false;
		for (const permission of out) {
			for (const required of requiredByPermission.get(permission) ?? []) {
				if (!out.has(required)) {
					out.add(required);
					changed = true;
				}
			}
		}
	}

	return [...out];
}
