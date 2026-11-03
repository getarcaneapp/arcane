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
