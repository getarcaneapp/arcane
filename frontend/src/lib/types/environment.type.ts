export type EnvironmentStatus = 'online' | 'standby' | 'offline' | 'error' | 'pending';

export type EdgeMTLSCertificate = {
	commonName?: string;
	expiresAt?: string;
	daysRemaining?: number;
	expired: boolean;
	expiringSoon: boolean;
};

export type Environment = {
	id: string;
	name: string;
	apiUrl: string;
	status: EnvironmentStatus;
	enabled: boolean;
	isEdge: boolean;
	edgeTransport?: 'grpc' | 'websocket';
	edgeSecurityMode?: 'token' | 'mtls';
	connected?: boolean;
	connectedAt?: string;
	lastHeartbeat?: string;
	lastPollAt?: string;
	lastSeen?: string;
	edgeMTLSCertificate?: EdgeMTLSCertificate;
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

export interface DeploymentSnippetFile {
	name: string;
	content: string;
	containerPath: string;
	permissions: string;
}

export interface DeploymentSnippetMTLS {
	dockerRun: string;
	dockerCompose: string;
	files: DeploymentSnippetFile[];
	hostDirHint: string;
}

export interface DeploymentSnippets {
	dockerRun: string;
	dockerCompose: string;
	mtls?: DeploymentSnippetMTLS;
}
