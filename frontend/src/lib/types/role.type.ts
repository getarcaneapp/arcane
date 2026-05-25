// Public RBAC types — mirrors backend types/role/role.go.

export type RoleScope = 'global' | 'env';

/** A named permission set. Built-in roles are immutable. */
export type Role = {
	id: string;
	name: string;
	description?: string;
	permissions: string[];
	builtIn: boolean;
	assignedUserCount: number;
	createdAt: string;
	updatedAt?: string;
};

export type CreateRole = {
	name: string;
	description?: string;
	permissions: string[];
};

export type UpdateRole = {
	name: string;
	description?: string;
	permissions: string[];
};

/** One row in a user's role assignments. EnvironmentID omitted = global scope. */
export type RoleAssignment = {
	id: string;
	userId: string;
	roleId: string;
	environmentId?: string;
	source: 'manual' | 'oidc';
	createdAt: string;
};

/** Compact assignment shape returned on the User payload. */
export type RoleAssignmentSummary = {
	roleId: string;
	environmentId?: string;
	source: 'manual' | 'oidc';
};

/** Payload for replacing a user's manual role assignments. */
export type SetUserAssignments = {
	assignments: { roleId: string; environmentId?: string }[];
};

export type OidcMappingSource = 'manual' | 'env';

export type OidcRoleMapping = {
	id: string;
	claimValue: string;
	roleId: string;
	environmentId?: string;
	source: OidcMappingSource;
	createdAt: string;
	updatedAt?: string;
};

export type CreateOidcRoleMapping = {
	claimValue: string;
	roleId: string;
	environmentId?: string;
};

export type UpdateOidcRoleMapping = CreateOidcRoleMapping;

/** The permission manifest from GET /roles/available-permissions. */
export type PermissionsManifest = {
	resources: PermissionResource[];
};

export type PermissionResource = {
	key: string;
	label: string;
	scope: RoleScope;
	actions: PermissionAction[];
};

export type PermissionAction = {
	key: string;
	permission: string;
	label: string;
	description?: string;
};

/** One permission grant on an API key. EnvironmentID omitted = global grant. */
export type ApiKeyPermissionGrant = {
	permission: string;
	environmentId?: string;
};

/** Built-in role IDs, stable across migrations. */
export const BUILT_IN_ROLE_ADMIN = 'role_admin';
export const BUILT_IN_ROLE_EDITOR = 'role_editor';
export const BUILT_IN_ROLE_DEPLOYER = 'role_deployer';
export const BUILT_IN_ROLE_VIEWER = 'role_viewer';

/** Sentinel used in PermissionsByEnv['global'] to mean "every permission". */
export const SUDO_PERMISSION = '*';

/** Map key used for global-scope permissions in PermissionsByEnv. */
export const GLOBAL_SCOPE = 'global';
