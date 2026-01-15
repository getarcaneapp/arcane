export interface SwarmServicePort {
	protocol: string;
	targetPort: number;
	publishedPort?: number;
	publishMode?: string;
}

export interface SwarmServiceSummary {
	id: string;
	name: string;
	image: string;
	mode: string;
	replicas: number;
	ports: SwarmServicePort[];
	createdAt: string;
	updatedAt: string;
	labels?: Record<string, string> | null;
	stackName?: string | null;
}

export interface SwarmServiceInspect {
	id: string;
	version: { index?: number; Index?: number };
	createdAt: string;
	updatedAt: string;
	spec: Record<string, unknown>;
	endpoint: Record<string, unknown>;
	updateStatus?: Record<string, unknown> | null;
}

export interface SwarmServiceCreateRequest {
	spec: Record<string, unknown>;
	options?: Record<string, unknown>;
}

export interface SwarmServiceUpdateRequest {
	version: number;
	spec: Record<string, unknown>;
	options?: Record<string, unknown>;
}

export interface SwarmServiceCreateResponse {
	id: string;
	warnings?: string[];
}

export interface SwarmServiceUpdateResponse {
	warnings?: string[];
}

export interface SwarmTaskSummary {
	id: string;
	name: string;
	serviceId: string;
	serviceName: string;
	nodeId: string;
	nodeName: string;
	desiredState: string;
	currentState: string;
	error?: string | null;
	containerId?: string | null;
	image?: string | null;
	slot?: number | null;
	createdAt: string;
	updatedAt: string;
}

export interface SwarmNodeSummary {
	id: string;
	hostname: string;
	role: string;
	availability: string;
	status: string;
	address?: string | null;
	managerStatus?: string | null;
	reachability?: string | null;
	labels?: Record<string, string> | null;
	engineVersion?: string | null;
	platform?: string | null;
	createdAt: string;
	updatedAt: string;
}

export interface SwarmStackSummary {
	id: string;
	name: string;
	namespace: string;
	services: number;
	createdAt: string;
	updatedAt: string;
}

export interface SwarmStackDeployRequest {
	name: string;
	composeContent: string;
	envContent?: string;
	withRegistryAuth?: boolean;
	prune?: boolean;
	resolveImage?: string;
}

export interface SwarmStackDeployResponse {
	name: string;
}

export interface SwarmInfo {
	id: string;
	createdAt: string;
	updatedAt: string;
	spec: Record<string, unknown>;
	rootRotationInProgress: boolean;
}
