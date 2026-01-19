export type EnvironmentStatus = 'online' | 'offline' | 'error' | 'pending';

export type Environment = {
	id: string;
	name: string;
	apiUrl: string;
	status: EnvironmentStatus;
	enabled: boolean;
	isEdge: boolean;
	lastSeen?: string;
	apiKey?: string;
};

export interface CreateEnvironmentDTO {
	apiUrl: string;
	name: string;
	bootstrapToken?: string;
	useApiKey?: boolean;
	isEdge?: boolean;
}

export interface UpdateEnvironmentDTO {
	apiUrl?: string;
	name?: string;
	enabled?: boolean;
	isEdge?: boolean;
	bootstrapToken?: string;
	regenerateApiKey?: boolean;
}

export interface DeploymentSnippets {
	dockerRun: string;
	dockerCompose: string;
}
