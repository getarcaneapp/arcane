export type OperationsCompatibility = 'current' | 'legacy';

export interface WorkloadCount {
	total: number;
	projects?: number;
	standaloneContainers?: number;
}

export interface OperationsState {
	updates?: WorkloadCount;
	stopped?: WorkloadCount;
	vulnerabilities?: number;
	expiringApiKeys?: number;
	compatibility: OperationsCompatibility;
}

export type OperationsStreamErrorCode = 'agent_incompatible' | 'unreachable' | '';

export interface OperationsStreamEvent {
	type: 'update' | 'error' | 'pending' | 'heartbeat';
	environmentId?: string;
	state?: OperationsState;
	error?: string;
	errorCode?: OperationsStreamErrorCode;
	timestamp: string;
}
