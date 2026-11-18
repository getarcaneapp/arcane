import type { EnvironmentSyncStatus } from '#lib/types/env.js';

export interface SecretSourceField {
	key: string;
	label: string;
	description?: string;
	placeholder?: string;
	default?: string;
	required?: boolean;
	multiline?: boolean;
}

export interface SecretSourceHTTPStep {
	query?: Record<string, string>;
	headers?: Record<string, string>;
	json?: Record<string, unknown>;
	form?: Record<string, string>;
	capture?: Record<string, string>;
	method: string;
	path: string;
}

export interface SecretSourceListMapping {
	key: string;
	value: string;
}

export interface SecretSourceExtract {
	list?: SecretSourceListMapping;
	from: string;
	object?: boolean;
}

export type SecretSourceKind = 'http' | 'container';

export interface SecretSourceDefinition {
	apiVersion: string;
	id: string;
	name: string;
	description?: string;
	kind: SecretSourceKind;
	pathRequired?: boolean;
	inputs: SecretSourceField[];
	credentials: SecretSourceField[];
	login?: SecretSourceHTTPStep;
	fetch?: SecretSourceHTTPStep;
	image?: string;
	env?: Record<string, string>;
	args?: string[];
	format?: string;
	extract?: SecretSourceExtract[];
}

export interface SecretSourcePreset {
	source: string;
	definition: SecretSourceDefinition;
}

export interface SecretSource {
	id: string;
	name: string;
	kind: SecretSourceKind;
	presetId?: string;
	definition: SecretSourceDefinition;
	inputs: Record<string, string>;
	credentialKeys: string[];
	path?: string;
	keyPrefix?: string;
	environmentIds: string[];
	allEnvironments: boolean;
	injectIntoContainers: boolean;
	enabled: boolean;
	lastError?: string;
	lastKeyCount: number;
	lastSkippedKeyCount: number;
	lastFetchedAt?: string;
	createdAt: string;
	updatedAt?: string;
}

export interface SecretSourceInput {
	name: string;
	presetId: string;
	definition: string;
	inputs: Record<string, string>;
	credentials: Record<string, string>;
	path: string;
	keyPrefix: string;
	environmentIds: string[];
	allEnvironments: boolean;
	injectIntoContainers: boolean;
	enabled: boolean;
}

export type SecretSourceUpdate = Partial<SecretSourceInput>;

export type SecretSourceTestInput = Partial<SecretSourceInput> & { id?: string };

export interface SecretSourceTestResult {
	success: boolean;
	keys: string[];
	keyCount: number;
	skippedKeyCount: number;
	error?: string;
}

export interface SecretSourceMutationResponse {
	source?: SecretSource;
	syncResults?: EnvironmentSyncStatus[];
}
