import type { FederatedCredential } from '../federated';
import type { CreateRequest, UpdateRequest } from './pagination';

export type FederatedCredentialWritable =
	| 'name'
	| 'description'
	| 'enabled'
	| 'issuerUrl'
	| 'audiences'
	| 'subjectClaim'
	| 'subjectMatch'
	| 'matchType'
	| 'roleId'
	| 'environmentId'
	| 'tokenTtlSeconds'
	| 'expiresAt';

export type FederatedCredentialCreate = CreateRequest<
	FederatedCredential,
	FederatedCredentialWritable,
	'name' | 'enabled' | 'issuerUrl' | 'audiences' | 'subjectMatch' | 'roleId'
>;

export type FederatedCredentialUpdate = UpdateRequest<FederatedCredential, FederatedCredentialWritable>;
