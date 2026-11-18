export type SecretSourceProvider = 'infisical' | 'bitwarden' | 'vault' | 'doppler' | 'onepassword' | 'protonpass' | 'http';

export type SecretSourceBitwardenScope = 'folder' | 'collection' | 'item';
export type SecretSourceOnePasswordScope = 'vault' | 'item';
export type SecretSourceVaultAuthMethod = 'token' | 'approle';

export interface SecretSourceInfisicalSettings {
	siteUrl: string;
	clientId: string;
	organizationSlug?: string;
	// Optional second identity used only by project setup to write to Infisical.
	setupClientId?: string;
}

export interface SecretSourceBitwardenSettings {
	serveUrl: string;
}

export interface SecretSourceVaultSettings {
	address: string;
	namespace?: string;
	authMethod: SecretSourceVaultAuthMethod;
	roleId?: string;
	appRoleMount?: string;
	// A separate token for guided setup is stored as the setup credential.
	setupToken?: boolean;
}

export interface SecretSourceDopplerSettings {
	apiUrl?: string;
}

export interface SecretSourceOnePasswordSettings {
	serverUrl: string;
}

export interface SecretSourceProtonPassSettings {
	kitUrl: string;
}

export interface SecretSourceHTTPSettings {
	baseUrl: string;
}

export interface SecretSourceSettings {
	infisical?: SecretSourceInfisicalSettings;
	bitwarden?: SecretSourceBitwardenSettings;
	vault?: SecretSourceVaultSettings;
	doppler?: SecretSourceDopplerSettings;
	onepassword?: SecretSourceOnePasswordSettings;
	protonpass?: SecretSourceProtonPassSettings;
	http?: SecretSourceHTTPSettings;
}

export interface SecretSource {
	id: string;
	name: string;
	provider: SecretSourceProvider;
	settings: SecretSourceSettings;
	hasCredential: boolean;
	hasSetupCredential: boolean;
	bindingCount: number;
	lastTestedAt?: string;
	lastTestError?: string;
	createdAt: string;
	updatedAt?: string;
}

export interface SecretSourceCreateRequest {
	name: string;
	provider: SecretSourceProvider;
	settings: SecretSourceSettings;
	credential?: string;
	setupCredential?: string;
}

export interface SecretSourceUpdateRequest {
	name?: string;
	settings?: SecretSourceSettings;
	credential?: string; // omitted or empty = keep stored credential
	setupCredential?: string; // omitted or empty = keep stored setup credential
	clearCredential?: boolean; // HTTP sources only: remove the stored bearer token
}

export interface SecretSourceTestRequest {
	sourceId?: string;
	provider: SecretSourceProvider;
	settings: SecretSourceSettings;
	credential?: string;
}

export interface SecretSourceTestResult {
	ok: boolean;
	message: string;
	canBrowse: boolean;
	visibleCount: number;
}

export interface SecretSourceBrowseQuery {
	kind: string;
	projectId?: string;
	environment?: string;
	path?: string;
	mount?: string;
	kvVersion?: number;
	vaultId?: string;
}

export interface SecretSourceBrowseItem {
	id: string;
	name: string;
	detail?: string;
	options?: SecretSourceBrowseItem[];
}

export interface SecretSourceInfisicalTarget {
	projectId: string;
	environment: string;
	secretPath: string;
	includeImports: boolean;
	expandReferences: boolean;
}

export interface SecretSourceBitwardenTarget {
	scope: SecretSourceBitwardenScope;
	id: string;
	name?: string;
}

export interface SecretSourceVaultTarget {
	mount: string;
	path: string;
	kvVersion: number;
}

export interface SecretSourceDopplerTarget {
	project?: string;
	config?: string;
}

export interface SecretSourceOnePasswordTarget {
	scope: SecretSourceOnePasswordScope;
	vaultId: string;
	itemId?: string;
	name?: string;
}

// Same shape as 1Password: a whole vault, or one item in it.
export type SecretSourceProtonPassTarget = SecretSourceOnePasswordTarget;

export interface SecretSourceHTTPTarget {
	path?: string;
}

export interface SecretSourceBindingTarget {
	infisical?: SecretSourceInfisicalTarget;
	bitwarden?: SecretSourceBitwardenTarget;
	vault?: SecretSourceVaultTarget;
	doppler?: SecretSourceDopplerTarget;
	onepassword?: SecretSourceOnePasswordTarget;
	protonpass?: SecretSourceProtonPassTarget;
	http?: SecretSourceHTTPTarget;
}

export interface SecretSourceBinding {
	id: string;
	projectId: string;
	// Bindings apply in position order; the earlier one wins on a shared key.
	position: number;
	sourceId: string;
	sourceName: string;
	provider: SecretSourceProvider;
	target: SecretSourceBindingTarget;
	required: boolean;
	enabled: boolean;
	autoRedeploy: boolean;
	redeployNeeded: boolean;
	deployedAt?: string;
	lastFetchedAt?: string;
	lastCheckedAt?: string;
	lastFetchError?: string;
}

export interface SecretSourceBindingUpsertRequest {
	sourceId: string;
	target: SecretSourceBindingTarget;
	required: boolean;
	enabled: boolean;
	autoRedeploy: boolean;
	position?: number;
}

export interface SecretSourceCheckResult {
	bindingId: string;
	// Set when this binding's fetch failed.
	error?: string;
	keys: string[];
	// Delivered by an earlier binding too; that binding's value is used.
	shadowedKeys: string[];
	// Not referenced by any service in the compose file.
	unusedKeys: string[];
	overriddenKeys: string[];
	invalidKeys: string[];
	redeployNeeded: boolean;
	neverDeployed: boolean;
	fetchedAt: string;
	deployedAt?: string;
}

// One target of a source, read once (for example to fill a new container's environment).
export interface SecretSourceTargetRef {
	sourceId: string;
	target: SecretSourceBindingTarget;
}

export interface SecretSourceProjectCheckResult {
	bindings: SecretSourceCheckResult[];
	redeployNeeded: boolean;
}

export type SecretSourceSetupMode =
	| 'new-project'
	| 'existing-project'
	| 'shared-folder'
	| 'new-folder'
	| 'existing-folder'
	| 'kv-path';
export type SecretSourceSetupValues = 'import' | 'placeholder';
export type SecretSourceSetupEnvFile = 'keep' | 'remove';
export type SecretSourceSetupRemoteState = 'missing' | 'same' | 'different' | 'exists' | 'unknown';
export type SecretSourceSetupStepStatus = 'done' | 'skipped' | 'failed' | 'pending';

export interface SecretSourceSetupTarget {
	mode: SecretSourceSetupMode;
	projectName?: string;
	projectId?: string;
	environment?: string;
	secretPath?: string;
	folderName?: string;
	folderId?: string;
	mount?: string;
	kvVersion?: number;
}

export interface SecretSourceSetupPlanRequest {
	sourceId: string;
	target?: SecretSourceSetupTarget;
}

export interface SecretSourceSetupVariable {
	key: string;
	inCompose: boolean;
	composeDefault: boolean;
	composeRequired: boolean;
	inEnvFile: boolean;
	envEmpty: boolean;
	fromGit: boolean;
	secretLike: boolean;
	remote: SecretSourceSetupRemoteState;
}

export interface SecretSourceSetupIdentity {
	id: string;
	name: string;
}

export interface SecretSourceSetupPlan {
	provider: SecretSourceProvider;
	suggestedProjectName: string;
	suggestedSecretPath: string;
	suggestedFolderName: string;
	canWrite: boolean;
	variables: SecretSourceSetupVariable[];
	projectNameTaken: boolean;
	remoteError?: string;
	deployIdentity?: SecretSourceSetupIdentity;
	deployIdentityError?: string;
	hasGitSource: boolean;
	alreadyBound: boolean;
}

export interface SecretSourceSetupApplyRequest {
	sourceId: string;
	target: SecretSourceSetupTarget;
	keys: string[];
	values: SecretSourceSetupValues;
	overwriteKeys: string[];
	envFile: SecretSourceSetupEnvFile;
	keepBackup: boolean;
	grantDeployIdentity: boolean;
	required: boolean;
	autoRedeploy: boolean;
}

export interface SecretSourceSetupStep {
	id: 'project' | 'folder' | 'secrets' | 'grant' | 'binding' | 'verify' | 'envFile';
	status: SecretSourceSetupStepStatus;
	detail?: string;
}

export interface SecretSourceSetupResult {
	ok: boolean;
	steps: SecretSourceSetupStep[];
	binding?: SecretSourceBinding;
	removedKeys?: string[];
	backupFile?: string;
}

export interface SecretSourceComposeService {
	name: string;
	available: string[];
	editable: boolean;
	reason?: string;
}

export interface SecretSourceComposeSkippedRef {
	service: string;
	key: string;
	reason: string;
}

export interface SecretSourceComposeRefsResult {
	compose: string;
	added: Record<string, string[]>;
	skipped: SecretSourceComposeSkippedRef[];
	// Set only when env keys were sent.
	env?: string;
	envAdded: string[];
}

export interface SecretSourceTargetKeys {
	keys: string[];
	skipped: string[];
}
