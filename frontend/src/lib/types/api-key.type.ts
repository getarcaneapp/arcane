import type { ApiKeyPermissionGrant } from '$lib/types/role.type';

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
