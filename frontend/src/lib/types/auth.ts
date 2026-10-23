import type { Locale } from '#lib/paraglide/runtime.js';
import type { Environment } from '#lib/types/environment.js';
import type { AppVersionInformation, ApplicationTheme, IconCatalog } from '#lib/types/settings.js';
import type { CreateRequest, Paginated, UpdateRequest } from '#lib/types/shared.js';

// --- RBAC: roles, permissions, assignments ---

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

export type RoleWritable = 'name' | 'description' | 'permissions';

export type RoleCreate = CreateRequest<Role, RoleWritable, 'name' | 'permissions'>;

export type RoleUpdate = UpdateRequest<Role, RoleWritable>;

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
	environmentId?: string | null;
	source: OidcMappingSource;
	createdAt: string;
	updatedAt?: string;
};

export type OidcRoleMappingWritable = 'claimValue' | 'roleId' | 'environmentId';

export type OidcRoleMappingCreate = CreateRequest<OidcRoleMapping, OidcRoleMappingWritable, 'claimValue' | 'roleId'>;

export type OidcRoleMappingUpdate = UpdateRequest<OidcRoleMapping, OidcRoleMappingWritable>;

export type PermissionsManifest = {
	resources: PermissionResource[];
	presets?: PermissionPreset[];
	accessSurfaces?: AccessSurface[];
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
	requires?: string[];
};

export type PermissionPreset = {
	key: string;
	label: string;
	description?: string;
	permissions: string[];
};

export type AccessSurfaceKind = 'route' | 'settings-category' | 'customize-category' | 'landing';
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

export type TimeFormat = 'auto' | '12h' | '24h';

export type ProjectEditorLayout = 'auto' | 'classic' | 'tree';

export type EnvironmentLabelStyle = 'name' | 'dot';

/**
 * Personal display/UI preferences stored on the user record. Every field is
 * optional: an unset value falls back to the frontend default.
 */
export type UserPreferences = {
	themeMode?: 'light' | 'dark' | 'system';
	applicationTheme?: ApplicationTheme;
	accentColor?: string;
	iconCatalog?: IconCatalog;
	oledMode?: boolean;
	glassEffectsEnabled?: boolean;
	animationsEnabled?: boolean;
	sidebarHoverExpansion?: boolean;
	keyboardShortcutsEnabled?: boolean;
	mobileNavigationMode?: 'floating' | 'docked';
	mobileNavigationShowLabels?: boolean;
	defaultLandingPage?: string;
	defaultProjectEditorLayout?: ProjectEditorLayout;
	environmentColors?: Record<string, string>;
	environmentLabelStyle?: EnvironmentLabelStyle;
};

export type User = {
	id: string;
	username: string;
	password?: string;
	displayName?: string | null;
	email?: string | null;
	avatarUrl?: string;
	roleAssignments: RoleAssignmentSummary[];
	permissionsByEnv: Record<string, string[]>;
	isGlobalAdmin: boolean;
	canDelete: boolean;
	createdAt: string;
	lastLogin?: string;
	updatedAt?: string;
	oidcSubjectId?: string;
	locale?: Locale | null;
	timeFormat: TimeFormat;
	fontSize?: number;
	preferences: UserPreferences;
	requiresPasswordChange: boolean;
};

export type UserWritable = 'username' | 'password' | 'displayName' | 'email' | 'locale' | 'timeFormat';

export type UserCreate = CreateRequest<User, UserWritable, 'username' | 'password'>;

export type UserUpdate = UpdateRequest<User, UserWritable>;

export type ProfileWritable = 'displayName' | 'email' | 'locale' | 'timeFormat' | 'fontSize' | 'preferences';

export type ProfileUpdate = UpdateRequest<User, ProfileWritable>;

// --- Auth: login, OIDC ---

export interface LoginCredentials {
	username: string;
	password: string;
}

export type AuthenticationStatus = 'authenticated' | 'mfa_required';

export type PasskeyLoginAvailability = {
	available: boolean;
};

export type PasskeyChallenge = {
	ceremonyId: string;
	transactionId?: string;
	options: Record<string, unknown>;
	expiresAt: string;
};

export type MobilePasskeyCompletion = {
	transactionId: string;
	expiresAt: string;
};

export type MFAChallenge = {
	transactionId: string;
	method: string;
	options: Record<string, unknown>;
	expiresAt: string;
};

export type AuthenticationResponse = {
	success: boolean;
	status: AuthenticationStatus;
	token?: string;
	expiresAt?: string;
	user?: User;
	mfa?: MFAChallenge;
	error?: string;
};

export type Passkey = {
	id: string;
	name: string;
	rpId: string;
	aaguid?: string;
	transports?: string[];
	backupEligible: boolean;
	backupState: boolean;
	cloneWarning: boolean;
	authenticatorAttachment?: string;
	createdAt: string;
	updatedAt?: string;
	lastUsedAt?: string;
};

export type PasskeyCapabilities = {
	passkeyMfaEnabled: boolean;
	passkeyCount: number;
	hasLocalPassword: boolean;
	hasOidcFallback: boolean;
	canEnrollWithActiveSession: boolean;
	canDeleteLastPasskey: boolean;
	requiresStepUp: boolean;
};

export type MFAStatus = {
	enabled: boolean;
	passkeyCount: number;
	recoveryCodesRemaining: number;
};

export type RecoveryCodesResponse = {
	codes: string[];
};

export type StepUpGrant = {
	token: string;
	expiresAt: string;
};

export interface AutoLoginConfig {
	enabled: boolean;
	username: string;
}

// --- API keys ---

export type ApiKey = {
	id: string;
	name: string;
	description?: string | null;
	keyPrefix: string;
	userId?: string;
	kind: 'scoped' | 'personal';
	isStatic: boolean;
	isBootstrap: boolean;
	expiresAt?: string | null;
	lastUsedAt?: string;
	createdAt: string;
	updatedAt?: string;
	permissions: ApiKeyPermissionGrant[] | null;
};

export type ApiKeyCreated = ApiKey & {
	key: string;
};

export type ApiKeyWritable = 'name' | 'description' | 'expiresAt' | 'permissions';

export type ApiKeyCreate = CreateRequest<ApiKey, ApiKeyWritable, 'name' | 'permissions'>;

export type PersonalApiKeyCreate = CreateRequest<ApiKey, Exclude<ApiKeyWritable, 'permissions'>, 'name'>;

export type ApiKeyUpdate = UpdateRequest<ApiKey, ApiKeyWritable>;

// --- Federated credentials ---

export type FederatedCredentialMatchType = 'exact' | 'glob';

export type FederatedCredential = {
	id: string;
	name: string;
	description?: string | null;
	enabled: boolean;
	issuerUrl: string;
	audiences: string[];
	subjectClaim: string;
	subjectMatch: string;
	matchType: FederatedCredentialMatchType;
	roleId: string;
	environmentId?: string | null;
	identityUserId: string;
	tokenTtlSeconds: number;
	lastUsedAt?: string;
	expiresAt?: string | null;
	createdAt: string;
	updatedAt?: string;
	serviceUsername?: string;
	roleName?: string;
	environmentName?: string;
};

export type FederatedCredentialWritable =
	| 'name'
	| 'description'
	| 'enabled'
	| 'issuerUrl'
	| 'audiences'
	| 'subjectClaim'
	| 'subjectMatch'
	| 'matchType'
	| 'roleId'
	| 'environmentId'
	| 'tokenTtlSeconds'
	| 'expiresAt';

export type FederatedCredentialCreate = CreateRequest<
	FederatedCredential,
	FederatedCredentialWritable,
	'name' | 'enabled' | 'issuerUrl' | 'audiences' | 'subjectMatch' | 'roleId'
>;

export type FederatedCredentialUpdate = UpdateRequest<FederatedCredential, FederatedCredentialWritable>;

export type SessionBootstrap = {
	user: User;
	version: Partial<AppVersionInformation>;
	environments: Paginated<Environment>;
	permissionsManifest: PermissionsManifest;
};
