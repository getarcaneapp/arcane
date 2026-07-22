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

export interface GlobalVariableCreateDto {
	key: string;
	value: string;
	isSecret?: boolean;
	allEnvironments?: boolean;
	environmentIds?: string[];
}

export interface GlobalVariableUpdateDto {
	key?: string;
	value?: string; // omitted for secrets = keep stored value
	isSecret?: boolean;
	allEnvironments?: boolean;
	environmentIds?: string[];
}

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
