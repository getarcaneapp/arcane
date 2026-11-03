import type { ContainerRegistry, ContainerRegistryRepository, ContainerRegistryRepositoryTag } from '../containerregistry';
import type { CreateRequest, UpdateRequest } from './pagination';

export type ContainerRegistryWritable =
	| 'url'
	| 'username'
	| 'description'
	| 'insecure'
	| 'enabled'
	| 'registryType'
	| 'repositoryNames'
	| 'awsAccessKeyId'
	| 'awsRegion'
	| 'token'
	| 'awsSecretAccessKey';

export type ContainerRegistryCreate = CreateRequest<ContainerRegistry, ContainerRegistryWritable, 'url'>;

export type ContainerRegistryUpdate = UpdateRequest<ContainerRegistry, ContainerRegistryWritable>;

export type ContainerRegistryRepositoryRow = ContainerRegistryRepository & { id: string };

export type ContainerRegistryRepositoryTagRow = ContainerRegistryRepositoryTag & { id: string };
