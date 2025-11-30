export type ApiKey = {
	id: string;
	name: string;
	description?: string;
	keyPrefix: string;
	userId: string;
	expiresAt?: string;
	lastUsedAt?: string;
	createdAt: string;
	updatedAt?: string;
};

export type ApiKeyCreated = ApiKey & {
	key: string;
};

export type CreateApiKey = {
	name: string;
	description?: string;
	expiresAt?: string;
};

export type UpdateApiKey = {
	name?: string;
	description?: string;
	expiresAt?: string;
};
