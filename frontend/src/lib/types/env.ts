export interface EnvVariable {
	key: string;
	value: string;
}

export interface GlobalVariable {
	id: string;
	key: string;
	value: string;
	isSecret: boolean;
	allEnvironments: boolean;
	injectIntoContainers: boolean;
	environmentIds: string[];
	createdAt: string;
	updatedAt?: string;
}

export interface EnvironmentSyncStatus {
	environmentId: string;
	environmentName?: string;
	status: 'synced' | 'pending' | 'error';
	error?: string;
	lastSyncedAt?: string;
}

export interface GlobalVariableMutationResponse {
	variable?: GlobalVariable;
	syncResults?: EnvironmentSyncStatus[];
}
