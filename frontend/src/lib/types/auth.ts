import type { Locale } from '#lib/paraglide/runtime.js';
import type { SelectorKind } from '#lib/permissions.generated.js';
import type { Environment } from '#lib/types/environment.js';
import type { AppVersionInformation, ApplicationTheme, IconCatalog } from '#lib/types/settings.js';
import type { CreateRequest, Paginated, UpdateRequest } from '#lib/types/shared.js';

// --- RBAC: roles, bindings, groups ---

export type RoleScope = 'global' | 'env';

export type Role = {
	id: string;
	name: string;
	description?: string | null;
	permissions: string[];
	builtIn: boolean;
	bindingCount: number;
	createdAt: string;
	updatedAt?: string;
};

export type RoleWritable = 'name' | 'description' | 'permissions';

export type RoleCreate = CreateRequest<Role, RoleWritable, 'name' | 'permissions'>;

export type RoleUpdate = UpdateRequest<Role, RoleWritable>;

export type ResourceSelector = {
	labels?: Record<string, string>;
	projects?: string[];
	ownerOnly?: boolean;
	names?: string[];
};

export type BindingSubjectType = 'user' | 'group';

export type BindingSource = 'manual' | 'federated' | 'oidc';

export type RoleBinding = {
	id: string;
	subjectType: BindingSubjectType;
	subjectId: string;
	subjectName: string;
	roleId: string;
	roleName: string;
	environmentId?: string;
	environmentName?: string;
	selector?: ResourceSelector;
	source: BindingSource;
	createdBy?: string;
	createdAt: string;
};

export type RoleBindingSummary = {
	id: string;
	roleId: string;
	roleName: string;
	environmentId?: string;
	selector?: ResourceSelector;
	source: BindingSource;
	groupId?: string;
	groupName?: string;
};

export type RoleBindingCreate = {
	subjectType: BindingSubjectType;
	subjectId: string;
	roleId: string;
	environmentId?: string;
	selector?: ResourceSelector;
};

export type RoleBindingCreateResult = {
	binding: RoleBinding;
	droppedPermissions: string[];
};

export type RoleBindingFilter = {
	subjectType?: BindingSubjectType;
	subjectId?: string;
	roleId?: string;
	environmentId?: string;
};

export type BindingRef = {
	id: string;
	roleId: string;
	roleName: string;
	groupId?: string;
	groupName?: string;
	unrestricted: boolean;
};

export type EffectivePermission = {
	permission: string;
	unrestricted: boolean;
	selectors?: ResourceSelector[];
	via: BindingRef[];
};

export type EffectiveScope = {
	environmentId?: string;
	permissions: EffectivePermission[];
};

export type EffectiveAccess = {
	userId: string;
	isGlobalAdmin: boolean;
	scopes: EffectiveScope[];
};

export type MembershipSource = 'manual' | 'oidc';

export type Group = {
	id: string;
	name: string;
	description?: string | null;
	memberCount: number;
	bindingCount: number;
	createdAt: string;
	updatedAt?: string;
};

export type GroupWritable = 'name' | 'description';

export type GroupCreate = CreateRequest<Group, GroupWritable, 'name'>;

export type GroupUpdate = UpdateRequest<Group, GroupWritable>;

export type GroupRef = {
	id: string;
	name: string;
	source: MembershipSource;
};

export type GroupMember = {
	userId: string;
	username: string;
	displayName?: string | null;
	source: MembershipSource;
	createdAt: string;
};

export type OidcMappingSource = 'manual' | 'env';

export type OidcGroupMapping = {
	id: string;
	claimValue: string;
	groupId: string;
	groupName: string;
	source: OidcMappingSource;
	createdAt: string;
	updatedAt?: string;
};

export type OidcGroupMappingSpec = Pick<OidcGroupMapping, 'claimValue' | 'groupId'>;

export type AuditActorType = 'user' | 'apikey' | 'agent' | 'system' | 'oidc';

export type AuditOutcome = 'success' | 'denied' | 'error';

export type AuditExportFormat = 'csv' | 'ndjson';

export type AuditLog = {
	id: string;
	createdAt: string;
	actorType: AuditActorType;
	actorUserId?: string;
	actorUsername?: string;
	apiKeyId?: string;
	action: string;
	outcome: AuditOutcome;
	targetType?: string;
	targetId?: string;
	targetName?: string;
	environmentId?: string;
	permission?: string;
	requestMethod?: string;
	requestPath?: string;
	remoteAddr?: string;
	userAgent?: string;
	before?: unknown;
	after?: unknown;
	metadata?: Record<string, unknown>;
};

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
	selectors?: SelectorKind[];
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
export type AccessScopeMode = 'global-only' | 'selected-env-plus-global' | 'any-effective-scope';

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
	allEnvironmentsPreview?: boolean;
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
	bindings: RoleBindingSummary[];
	groups: GroupRef[];
	permissionsByEnv: Record<string, string[]>;
	scopedPermissionsByEnv: Record<string, string[]>;
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
