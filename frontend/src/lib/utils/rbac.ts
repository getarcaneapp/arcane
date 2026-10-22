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
