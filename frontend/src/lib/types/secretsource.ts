export type SecretSourceProvider = 'infisical';

export interface SecretSourceInfisicalSettings {
	siteUrl: string;
	clientId: string;
	organizationSlug?: string;
}

export interface SecretSourceSettings {
	infisical?: SecretSourceInfisicalSettings;
}

export interface SecretSource {
	id: string;
	name: string;
	provider: SecretSourceProvider;
	settings: SecretSourceSettings;
	hasCredential: boolean;
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
}

export interface SecretSourceUpdateRequest {
	name?: string;
	settings?: SecretSourceSettings;
	credential?: string; // omitted or empty = keep stored credential
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

export interface SecretSourceBindingTarget {
	infisical?: SecretSourceInfisicalTarget;
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
	overriddenKeys: string[];
	invalidKeys: string[];
	redeployNeeded: boolean;
	neverDeployed: boolean;
	fetchedAt: string;
	deployedAt?: string;
}

export interface SecretSourceProjectCheckResult {
	bindings: SecretSourceCheckResult[];
	redeployNeeded: boolean;
}
