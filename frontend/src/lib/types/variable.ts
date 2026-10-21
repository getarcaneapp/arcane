import type { CreateRequest, UpdateRequest } from './shared';

export interface GlobalVariable {
	id: string;
	key: string;
	value: string; // '' when isSecret (server redacts)
	isSecret: boolean;
	allEnvironments: boolean;
	environmentIds: string[];
	createdAt: string;
	updatedAt?: string;
}

export type GlobalVariableWritable = 'key' | 'value' | 'isSecret' | 'allEnvironments' | 'environmentIds';

export type GlobalVariableCreate = CreateRequest<GlobalVariable, GlobalVariableWritable, 'key' | 'value'>;

export type GlobalVariableUpdate = UpdateRequest<GlobalVariable, GlobalVariableWritable>;

export interface VariableEnvSyncResult {
	environmentId: string;
	environmentName?: string;
	status: 'synced' | 'pending' | 'error';
	error?: string;
	lastSyncedAt?: string;
}

export interface VariableMutationResponse {
	variable?: GlobalVariable;
	syncResults?: VariableEnvSyncResult[];
}
