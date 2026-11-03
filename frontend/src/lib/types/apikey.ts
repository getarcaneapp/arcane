export type ApiKeyPermissionGrant = {
	permission: string;
	environmentId?: string;
};

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
	permissions: ApiKeyPermissionGrant[];
};

export type ApiKeyCreatedDto = ApiKey & {
	key: string;
};
