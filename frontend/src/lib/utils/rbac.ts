import type { KeyValueRow } from '#lib/components/form/key-value-editor.svelte';
import { m } from '#lib/paraglide/messages.js';
import type { SelectorKind } from '#lib/permissions.generated.js';
import type { PermissionsManifest, ResourceSelector } from '#lib/types/auth.js';

export type RoleBadgeVariant = 'red' | 'blue' | 'purple' | 'amber' | 'gray' | 'green';

const BUILT_IN_ROLE_VARIANTS: Record<string, RoleBadgeVariant> = {
	role_admin: 'red',
	role_editor: 'blue',
	role_no_shell_editor: 'blue',
	role_deployer: 'purple',
	role_monitor: 'amber',
	role_viewer: 'gray'
};

export function roleBadgeVariant(roleId: string): RoleBadgeVariant {
	return BUILT_IN_ROLE_VARIANTS[roleId] ?? 'green';
}

export function selectorKindsByPermission(manifest: PermissionsManifest | undefined): Map<string, readonly SelectorKind[]> {
	return new Map(
		(manifest?.resources ?? []).flatMap((resource) =>
			resource.actions.map((action) => [action.permission, action.selectors ?? []] as const)
		)
	);
}

export function selectorKindsForPermissions(
	kindsByPermission: Map<string, readonly SelectorKind[]>,
	permissions: readonly string[]
): SelectorKind[] {
	const kinds = new Set<SelectorKind>();
	for (const permission of permissions) {
		for (const kind of kindsByPermission.get(permission) ?? []) kinds.add(kind);
	}
	return [...kinds];
}

export function isEmptySelector(selector: ResourceSelector | undefined): boolean {
	return (
		!selector ||
		(Object.keys(selector.labels ?? {}).length === 0 &&
			(selector.projects ?? []).length === 0 &&
			!selector.ownerOnly &&
			(selector.names ?? []).length === 0)
	);
}

export function selectorSummary(selector: ResourceSelector | undefined): string[] {
	const labels = Object.entries(selector?.labels ?? {}).map(([key, value]) => `${key}=${value}`);
	const projects = selector?.projects ?? [];
	const names = selector?.names ?? [];
	return [
		...(labels.length > 0 ? [m.selector_summary_labels({ value: labels.join(', ') })] : []),
		...(projects.length > 0 ? [m.selector_summary_projects({ value: projects.join(', ') })] : []),
		...(selector?.ownerOnly ? [m.selector_summary_owner()] : []),
		...(names.length > 0 ? [m.selector_summary_names({ value: names.join(', ') })] : [])
	];
}

export type SelectorDraft = {
	labels: KeyValueRow[];
	projects: string[];
	ownerOnly: boolean;
	names: string[];
};

export function emptySelectorDraft(): SelectorDraft {
	return { labels: [], projects: [], ownerOnly: false, names: [] };
}

export function selectorFromDraft(draft: SelectorDraft, kinds: readonly SelectorKind[]): ResourceSelector | undefined {
	const labels = Object.fromEntries(
		draft.labels.map((row) => [row.key.trim(), row.value.trim()] as const).filter(([key]) => key.length > 0)
	);
	const selector: ResourceSelector = {
		labels: kinds.includes('labels') && Object.keys(labels).length > 0 ? labels : undefined,
		projects: kinds.includes('projects') && draft.projects.length > 0 ? [...draft.projects] : undefined,
		ownerOnly: kinds.includes('owner') && draft.ownerOnly ? true : undefined,
		names: kinds.includes('names') && draft.names.length > 0 ? [...draft.names] : undefined
	};
	return isEmptySelector(selector) ? undefined : selector;
}
