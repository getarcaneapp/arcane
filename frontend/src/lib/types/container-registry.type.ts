export type RegistryType = 'generic' | 'ecr';

export interface ContainerRegistryCreateDto {
	url: string;
	username?: string;
	token?: string;
	description?: string;
	insecure?: boolean;
	enabled?: boolean;
	registryType?: RegistryType;
	awsAccessKeyId?: string;
	awsSecretAccessKey?: string;
	awsRegion?: string;
}

export interface ContainerRegistryUpdateDto {
	url?: string;
	username?: string;
	token?: string;
	description?: string;
	insecure?: boolean;
	enabled?: boolean;
	registryType?: RegistryType;
	awsAccessKeyId?: string;
	awsSecretAccessKey?: string;
	awsRegion?: string;
}

export interface ContainerRegistry {
	id: string;
	url: string;
	username: string;
	token: string;
	description?: string;
	insecure?: boolean;
	enabled?: boolean;
	registryType?: RegistryType;
	awsAccessKeyId?: string;
	awsRegion?: string;
	createdAt?: string;
	updatedAt?: string;
}

export interface ContainerRegistryPullUsageResponse {
	registries: ContainerRegistryPullUsage[];
}

export interface ContainerRegistryPullUsage {
	registryId: string;
	provider: string;
	registry: string;
	displayName: string;
	repository?: string;
	limit?: number;
	remaining?: number;
	used?: number;
	windowSeconds?: number;
	observedPulls: number;
	authMethod: 'anonymous' | 'credential' | 'unknown';
	authUsername?: string;
	source?: string;
	checkedAt: string;
	error?: string;
}
