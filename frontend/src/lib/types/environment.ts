export type EnvironmentStatus = 'online' | 'offline' | 'error' | 'pending';

export type AgentMTLSCertificate = {
	commonName?: string;
	expiresAt?: string;
	daysRemaining?: number;
	expired: boolean;
	expiringSoon: boolean;
};

export type AgentHostInfo = {
	hostname?: string;
	os?: string;
	kernelVersion?: string;
	architecture?: string;
	dockerVersion?: string;
	agentVersion?: string;
	publicIp?: string;
	memoryBytes?: number;
	cpus?: number;
};

export type AgentTunnelStats = {
	roundTripMinMs?: number;
	roundTripAvgMs?: number;
	roundTripMaxMs?: number;
	lastDisconnectAt?: string;
	lastDisconnectReason?: string;
	bytesIn: number;
	bytesOut: number;
	streamsOpened: number;
	sessions: number;
};

export type Environment = {
	id: string;
	name: string;
	apiUrl: string;
	status: EnvironmentStatus;
	enabled: boolean;
	agentTransport?: 'grpc' | 'websocket';
	lastAgentTransport?: 'grpc' | 'websocket';
	agentSecurityMode?: 'token' | 'mtls';
	agentRoundTripMs?: number;
	agentOpenStreams?: number;
	agentSessionId?: string;
	agentInstance?: string;
	agentCapabilities?: string[];
	connected?: boolean;
	connectedAt?: string;
	lastHeartbeat?: string;
	lastSeen?: string;
	agentMTLSCertificate?: AgentMTLSCertificate;
	agentHost?: AgentHostInfo;
	agentTunnelStats?: AgentTunnelStats;
};

export type EnvironmentStreamEvent = {
	type: string;
	environments?: Environment[];
};

export interface DeploymentSnippetFile {
	name: string;
	content?: string;
	downloadUrl?: string;
	containerPath: string;
	permissions: string;
}

export interface DeploymentSnippetMTLS {
	dockerRun: string;
	dockerCompose: string;
	files: DeploymentSnippetFile[];
	hostDirHint: string;
}

export interface DeploymentSnippet {
	dockerRun: string;
	dockerCompose: string;
	enrollmentToken: string;
	mtls?: DeploymentSnippetMTLS;
}

export interface EnrollmentToken {
	id: string;
	name: string;
	environmentId?: string;
	environmentName?: string;
	maxUses?: number;
	useCount: number;
	createdAt: string;
	lastUsedAt?: string;
}

export interface EnrollmentTokenCreate {
	name: string;
	maxUses?: number;
}

export interface EnrollmentTokenCreated {
	id: string;
	name: string;
	maxUses?: number;
	createdAt: string;
	token: string;
}

export interface EnrollmentTokenSecret {
	token: string;
}
