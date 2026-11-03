export type RegistryType = 'generic' | 'ecr';

export interface ContainerRegistryRepository {
	name: string;
}

export interface ContainerRegistryRepositoryTag {
	created?: string;
	name: string;
	digest?: string;
	mediaType?: string;
	error?: string;
	platforms: ContainerRegistryTagPlatform[];
	size: number;
}

export interface ContainerRegistryTagPlatform {
	os: string;
	architecture: string;
	variant?: string;
	digest: string;
	size: number;
}

export interface ContainerRegistry {
	createdAt: string;
	updatedAt: string;
	description?: string;
	id: string;
	url: string;
	host: string;
	namespace: string;
	username: string;
	registryType: RegistryType;
	awsAccessKeyId?: string;
	awsRegion?: string;
	token?: string;
	awsSecretAccessKey?: string;
	repositoryNames: string[];
	insecure: boolean;
	enabled: boolean;
}

export interface ContainerRegistryPullUsageResponse {
	registries: ContainerRegistryPullUsage[];
}

export interface ContainerRegistryPullUsage {
	checkedAt: string;
	limit?: number;
	remaining?: number;
	used?: number;
	windowSeconds?: number;
	registryId: string;
	provider: string;
	registry: string;
	displayName: string;
	repository?: string;
	authMethod: 'anonymous' | 'credential' | 'unknown';
	authUsername?: string;
	source?: string;
	error?: string;
	observedPulls: number;
}

export interface ContainerRegistryCredential {
	url: string;
	username: string;
	token: string;
	enabled: boolean;
}
