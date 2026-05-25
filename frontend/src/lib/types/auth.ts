import type { Locale } from '$lib/paraglide/runtime';

// --- RBAC: roles, permissions, assignments ---

export type RoleScope = 'global' | 'env';

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

export type RoleAssignment = {
	id: string;
	userId: string;
	roleId: string;
	environmentId?: string;
	source: 'manual' | 'oidc';
	createdAt: string;
};

export type RoleAssignmentSummary = {
	roleId: string;
	environmentId?: string;
	source: 'manual' | 'oidc';
};

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

export type ApiKeyPermissionGrant = {
	permission: string;
	environmentId?: string;
};

export const BUILT_IN_ROLE_ADMIN = 'role_admin';
export const BUILT_IN_ROLE_EDITOR = 'role_editor';
export const BUILT_IN_ROLE_DEPLOYER = 'role_deployer';
export const BUILT_IN_ROLE_VIEWER = 'role_viewer';

export const SUDO_PERMISSION = '*';

export const GLOBAL_SCOPE = 'global';

// --- User ---

export type User = {
	id: string;
	username: string;
	passwordHash?: string;
	displayName?: string;
	email?: string;
	roleAssignments: RoleAssignmentSummary[];
	permissionsByEnv: Record<string, string[]>;
	canDelete?: boolean;
	createdAt: string;
	lastLogin?: string;
	updatedAt?: string;
	oidcSubjectId?: string;
	locale?: Locale;
	requiresPasswordChange?: boolean;
};

export type CreateUser = Omit<
	User,
	| 'id'
	| 'createdAt'
	| 'updatedAt'
	| 'lastLogin'
	| 'oidcSubjectId'
	| 'passwordHash'
	| 'requiresPasswordChange'
	| 'roleAssignments'
	| 'permissionsByEnv'
> & {
	password: string;
};

// --- Auth: login, OIDC ---

export interface OidcUserInfo {
	sub: string;
	email: string;
	name?: string;
	displayName?: string;
	preferred_username?: string;
	given_name?: string;
	family_name?: string;
	picture?: string;
	groups?: string[];
}

export interface LoginCredentials {
	username: string;
	password: string;
}

export type LoginResponseData = {
	token: string;
	refreshToken: string;
	expiresAt: string;
	user: User;
	requirePasswordChange?: boolean;
};

export interface AutoLoginConfig {
	enabled: boolean;
	username: string;
}

// --- API keys ---

export type ApiKey = {
	id: string;
	name: string;
	description?: string;
	keyPrefix: string;
	userId: string;
	isStatic: boolean;
	isBootstrap: boolean;
	expiresAt?: string;
	lastUsedAt?: string;
	createdAt: string;
	updatedAt?: string;
	permissions?: ApiKeyPermissionGrant[];
};

export type ApiKeyCreated = ApiKey & {
	key: string;
};

export type CreateApiKey = {
	name: string;
	description?: string;
	expiresAt?: string;
	permissions: ApiKeyPermissionGrant[];
};

export type UpdateApiKey = {
	name?: string;
	description?: string;
	expiresAt?: string;
	permissions?: ApiKeyPermissionGrant[];
};
