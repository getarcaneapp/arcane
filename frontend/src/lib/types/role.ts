export type RoleScope = 'global' | 'env';

export type Role = {
	id: string;
	name: string;
	description?: string | null;
	permissions: string[];
	builtIn: boolean;
	assignedUserCount: number;
	createdAt: string;
	updatedAt?: string;
};

export type RoleAssignment = {
	id: string;
	userId: string;
	roleId: string;
	environmentId?: string;
	source: 'manual' | 'oidc';
	createdAt: string;
};

export type UserRoleAssignmentInput = {
	roleId: string;
	environmentId?: string;
};

export type SetUserRoleAssignments = {
	assignments: UserRoleAssignmentInput[];
};

export type OidcMappingSource = 'manual' | 'env';

export type OidcRoleMapping = {
	id: string;
	claimValue: string;
	roleId: string;
	environmentId?: string | null;
	source: OidcMappingSource;
	createdAt: string;
	updatedAt?: string;
};

export type PermissionsManifest = {
	resources: PermissionResource[];
	presets?: PermissionPreset[];
	accessSurfaces?: AccessSurface[];
};

export type PermissionResource = {
	key: string;
	label: string;
	description: string;
	scope: RoleScope;
	actions: PermissionAction[];
};

export type PermissionAction = {
	key: string;
	permission: string;
	label: string;
	description?: string;
	requires?: string[];
};

export type PermissionPreset = {
	key: string;
	label: string;
	description?: string;
	permissions: string[];
};

export type AccessSurfaceKind = 'route' | 'settings-category' | 'landing';
export type AccessMode = 'permissions' | 'any-child';
export type AccessMatchMode = 'any-of' | 'all-of';
export type AccessScopeMode = 'global-only' | 'env-plus-global' | 'any-effective-scope';

export type AccessSurface = {
	id: string;
	kind: AccessSurfaceKind;
	url?: string;
	label: string;
	accessMode: AccessMode;
	matchMode: AccessMatchMode;
	scopeMode: AccessScopeMode;
	permissions?: string[];
	children?: string[];
	fallbackOrder?: number;
};

export const BUILT_IN_ROLE_ADMIN = 'role_admin';
export const BUILT_IN_ROLE_EDITOR = 'role_editor';
export const BUILT_IN_ROLE_DEPLOYER = 'role_deployer';
export const BUILT_IN_ROLE_VIEWER = 'role_viewer';

export const SUDO_PERMISSION = '*';

export const GLOBAL_SCOPE = 'global';
