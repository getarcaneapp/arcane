export interface SwarmServicePort {
	protocol: string;
	targetPort: number;
	publishedPort?: number;
	publishMode?: string;
}

export interface SwarmServiceMount {
	type: string;
	source?: string;
	target: string;
	readOnly?: boolean;
	volumeDriver?: string;
	volumeOptions?: Record<string, string>;
	devicePath?: string;
}

export interface SwarmServiceSummary {
	id: string;
	name: string;
	image: string;
	mode: string;
	replicas: number;
	runningReplicas: number;
	ports: SwarmServicePort[];
	createdAt: string;
	updatedAt: string;
	labels?: Record<string, string> | null;
	stackName?: string | null;
	nodes: string[];
	networks: string[];
	mounts: SwarmServiceMount[];
}

export interface ServiceNetworkIPAMConfig {
	subnet?: string;
	gateway?: string;
	ipRange?: string;
}

export interface ServiceNetworkConfigDetail {
	name: string;
	driver: string;
	scope: string;
	enableIPv4: boolean;
	enableIPv6: boolean;
	options?: Record<string, string>;
	ipv4Configs?: ServiceNetworkIPAMConfig[];
	ipv6Configs?: ServiceNetworkIPAMConfig[];
}

export interface ServiceNetworkDetail {
	id: string;
	name: string;
	driver: string;
	scope: string;
	internal: boolean;
	attachable: boolean;
	ingress: boolean;
	enableIPv4: boolean;
	enableIPv6: boolean;
	configFrom?: string;
	configOnly: boolean;
	options?: Record<string, string>;
	ipamConfigs?: ServiceNetworkIPAMConfig[];
	configNetwork?: ServiceNetworkConfigDetail | null;
}

export interface SwarmServiceInspect {
	id: string;
	version: { index?: number; Index?: number };
	createdAt: string;
	updatedAt: string;
	spec: Record<string, unknown>;
	endpoint: Record<string, unknown>;
	updateStatus?: Record<string, unknown> | null;
	nodes?: string[];
	networkDetails?: Record<string, ServiceNetworkDetail>;
	mounts?: SwarmServiceMount[];
}

export interface SwarmServiceCreateOptions {
	encodedRegistryAuth?: string;
	queryRegistry?: boolean;
}

export interface SwarmServiceUpdateOptions {
	encodedRegistryAuth?: string;
	registryAuthFrom?: 'spec' | 'previous-spec';
	rollback?: 'previous' | 'none' | string;
	queryRegistry?: boolean;
}

export interface SwarmServiceCreateRequest {
	spec: Record<string, unknown>;
	options?: SwarmServiceCreateOptions;
}

export interface SwarmServiceUpdateRequest {
	version: number;
	spec: Record<string, unknown>;
	options?: SwarmServiceUpdateOptions;
}

export interface SwarmServiceScaleRequest {
	replicas: number;
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

export interface SwarmNodeUpdateRequest {
	version?: number;
	name?: string;
	labels?: Record<string, string>;
	role?: 'manager' | 'worker';
	availability?: 'active' | 'pause' | 'drain';
}

export interface SwarmStackSummary {
	id: string;
	name: string;
	namespace: string;
	services: number;
	createdAt: string;
	updatedAt: string;
}

export interface SwarmStackInspect {
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

export interface SwarmStackRenderConfigRequest {
	name: string;
	composeContent: string;
	envContent?: string;
}

export interface SwarmStackRenderConfigResponse {
	name: string;
	renderedCompose: string;
	services: string[];
	networks: string[];
	volumes: string[];
	configs: string[];
	secrets: string[];
	warnings?: string[];
}

export interface SwarmStackSource {
	name: string;
	composeContent: string;
	envContent?: string;
}

export interface SwarmInfo {
	id: string;
	createdAt: string;
	updatedAt: string;
	spec: Record<string, unknown>;
	rootRotationInProgress: boolean;
}

export interface SwarmInitRequest {
	listenAddr?: string;
	advertiseAddr?: string;
	dataPathAddr?: string;
	dataPathPort?: number;
	forceNewCluster?: boolean;
	spec: Record<string, unknown>;
	autoLockManagers?: boolean;
	availability?: 'active' | 'pause' | 'drain';
	defaultAddrPool?: string[];
	subnetSize?: number;
}

export interface SwarmInitResponse {
	nodeId: string;
}

export interface SwarmJoinRequest {
	listenAddr?: string;
	advertiseAddr?: string;
	dataPathAddr?: string;
	remoteAddrs: string[];
	joinToken: string;
	availability?: 'active' | 'pause' | 'drain';
}

export interface SwarmLeaveRequest {
	force?: boolean;
}

export interface SwarmUnlockRequest {
	key: string;
}

export interface SwarmUnlockKeyResponse {
	unlockKey: string;
}

export interface SwarmJoinTokensResponse {
	worker: string;
	manager: string;
}

export interface SwarmRotateJoinTokensRequest {
	rotateWorkerToken?: boolean;
	rotateManagerToken?: boolean;
}

export interface SwarmUpdateRequest {
	version?: number;
	spec: Record<string, unknown>;
	rotateWorkerToken?: boolean;
	rotateManagerToken?: boolean;
	rotateManagerUnlockKey?: boolean;
}

export interface SwarmConfigSummary {
	id: string;
	version: { index?: number; Index?: number };
	createdAt: string;
	updatedAt: string;
	spec: Record<string, unknown>;
}

export interface SwarmSecretSummary {
	id: string;
	version: { index?: number; Index?: number };
	createdAt: string;
	updatedAt: string;
	spec: Record<string, unknown>;
}

export interface SwarmConfigCreateRequest {
	spec: Record<string, unknown>;
}

export interface SwarmConfigUpdateRequest {
	version?: number;
	spec: Record<string, unknown>;
}

export interface SwarmSecretCreateRequest {
	spec: Record<string, unknown>;
}

export interface SwarmSecretUpdateRequest {
	version?: number;
	spec: Record<string, unknown>;
}
