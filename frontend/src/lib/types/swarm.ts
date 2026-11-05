export interface SwarmRuntimeStatus {
	enabled: boolean;
	clusterId?: string;
}

export interface SwarmInfo {
	id: string;
	createdAt: string;
	updatedAt: string;
	spec: Record<string, unknown>;
	rootRotationInProgress: boolean;
}

export interface SwarmKvSpec {
	Name: string;
	Labels?: Record<string, string> | null;
	Data?: string | null;
	Driver?: { Name: string; Options?: Record<string, string> | null } | null;
}

export interface SwarmConfig {
	id: string;
	environmentId?: string;
	environmentName?: string;
	version: { Index?: number };
	createdAt: string;
	updatedAt: string;
	spec: SwarmKvSpec;
}

export interface SwarmSecret {
	id: string;
	environmentId?: string;
	environmentName?: string;
	version: { Index?: number };
	createdAt: string;
	updatedAt: string;
	spec: SwarmKvSpec;
}

export interface SwarmConfigUpdateRequest {
	version: number;
	labels: Record<string, string>;
}

export interface SwarmSecretUpdateRequest {
	version: number;
	labels: Record<string, string>;
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

export interface NodeAgentReconcileResult {
	nodeId: string;
	state: NodeAgentState;
	environmentId?: string;
	candidates?: NodeAgentCandidate[];
}

export interface NodeAgentReconcileResponse {
	results: NodeAgentReconcileResult[];
}

export interface NodeAgentBindingRequest {
	environmentId: string;
	rebind?: boolean;
	replaceDeployment?: boolean;
}

export type SwarmJoinEnvironmentRole = 'worker' | 'manager';

export type SwarmJoinEnvironmentResultState = 'joined' | 'already_member' | 'joined_unverified' | 'failed';

export interface SwarmJoinCandidate {
	environmentId: string;
	environmentName: string;
	environmentType: string;
	status: string;
}

export interface SwarmJoinEnvironmentTarget {
	environmentId: string;
	role: SwarmJoinEnvironmentRole;
	availability?: 'active' | 'pause' | 'drain';
	listenAddr?: string;
	advertiseAddr?: string;
	dataPathAddr?: string;
}

export interface SwarmJoinEnvironmentsRequest {
	remoteAddrs: string[];
	targets: SwarmJoinEnvironmentTarget[];
}

export interface SwarmJoinEnvironmentResult {
	environmentId: string;
	state: SwarmJoinEnvironmentResultState;
	nodeId?: string;
	error?: string;
}

export interface SwarmJoinEnvironmentsResponse {
	results: SwarmJoinEnvironmentResult[];
}

export type NodeAgentState = 'none' | 'pending' | 'offline' | 'connected' | 'mismatched' | 'ambiguous';

export type NodeAgentBindingKind = 'local' | 'environment' | 'dedicated';

export interface NodeAgentCandidate {
	environmentId: string;
	environmentName: string;
	environmentType: string;
}

export interface NodeAgentStatus {
	state: NodeAgentState;
	bindingKind?: NodeAgentBindingKind;
	environmentId?: string;
	environmentName?: string;
	environmentType?: string;
	connected?: boolean;
	lastHeartbeat?: string;
	reportedNodeId?: string;
	reportedHostname?: string;
	candidates?: NodeAgentCandidate[];
}

export interface SwarmNodeAgentDeployment {
	environmentId: string;
	agent: NodeAgentStatus;
	dockerRun: string;
	dockerCompose: string;
	enrollmentToken: string;
}

export interface SwarmNode {
	id: string;
	environmentId?: string;
	environmentName?: string;
	name?: string;
	version?: number;
	hostname: string;
	role: string;
	availability: string;
	status: string;
	agent: NodeAgentStatus;
	address?: string;
	managerStatus?: string;
	managerAddress?: string;
	reachability?: string;
	labels?: Record<string, string>;
	systemLabels?: Record<string, string>;
	engineVersion?: string;
	platform?: string;
	createdAt: string;
	updatedAt: string;
}

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

export type SwarmServiceModeName = 'replicated' | 'global' | 'replicated-job' | 'global-job' | 'unknown';

export interface SwarmServiceSummary {
	id: string;
	environmentId?: string;
	environmentName?: string;
	name: string;
	image: string;
	mode: SwarmServiceModeName;
	replicas: number;
	runningReplicas: number;
	ports: SwarmServicePort[];
	createdAt: string;
	updatedAt: string;
	labels: Record<string, string>;
	stackName?: string;
	nodes: string[];
	networks: string[];
	mounts: SwarmServiceMount[];
}

export interface SwarmReplicatedMode {
	Replicas?: number;
}

export interface SwarmReplicatedJobMode {
	MaxConcurrent?: number;
	TotalCompletions?: number;
}

export interface SwarmServiceModeSpec {
	Replicated?: SwarmReplicatedMode;
	Global?: Record<string, never>;
	ReplicatedJob?: SwarmReplicatedJobMode;
	GlobalJob?: Record<string, never>;
}

export interface SwarmServiceInspect {
	id: string;
	version: { index?: number; Index?: number };
	createdAt: string;
	updatedAt: string;
	spec: Record<string, unknown>;
	endpoint: Record<string, unknown>;
	updateStatus?: Record<string, unknown>;
	nodes?: string[];
	networkDetails?: Record<string, SwarmServiceNetworkDetail>;
	mounts?: SwarmServiceMount[];
}

export interface SwarmServiceNetworkIPAMConfig {
	subnet?: string;
	gateway?: string;
	ipRange?: string;
}

export interface SwarmServiceNetworkConfigDetail {
	name: string;
	driver: string;
	scope: string;
	enableIPv4: boolean;
	enableIPv6: boolean;
	options?: Record<string, string>;
	ipv4Configs?: SwarmServiceNetworkIPAMConfig[];
	ipv6Configs?: SwarmServiceNetworkIPAMConfig[];
}

export interface SwarmServiceNetworkDetail {
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
	ipamConfigs?: SwarmServiceNetworkIPAMConfig[];
	configNetwork?: SwarmServiceNetworkConfigDetail;
}

export interface SwarmServiceCreateMountSpec {
	Type: 'volume' | 'bind';
	Source: string;
	Target: string;
}

export interface SwarmServiceCreatePortSpec {
	TargetPort: number;
	PublishedPort?: number;
	Protocol: 'tcp' | 'udp';
}

export interface SwarmServiceCreateContainerSpec {
	Image: string;
	Command?: string[];
	Args?: string[];
	Dir?: string;
	User?: string;
	Hostname?: string;
	Env?: string[];
	Mounts?: SwarmServiceCreateMountSpec[];
}

export interface SwarmServiceCreateSpec {
	Name: string;
	TaskTemplate: {
		ContainerSpec: SwarmServiceCreateContainerSpec;
	};
	Mode: { Replicated: { Replicas: number } } | { Global: Record<string, never> };
	Labels?: Record<string, string>;
	EndpointSpec?: {
		Ports: SwarmServiceCreatePortSpec[];
	};
}

export interface SwarmServiceCreateRequest {
	spec: SwarmServiceCreateSpec;
	options?: SwarmServiceCreateOptions;
}

export interface SwarmServiceUpdateRequest {
	version: number;
	spec: Record<string, unknown>;
	options?: SwarmServiceUpdateOptions;
}

export interface SwarmServiceCreateResponse {
	id: string;
	warnings?: string[];
}

export interface SwarmServiceUpdateResponse {
	warnings?: string[];
}

export interface SwarmServiceCreateOptions {
	encodedRegistryAuth?: string;
	queryRegistry?: boolean;
}

export interface SwarmServiceUpdateOptions {
	encodedRegistryAuth?: string;
	registryAuthFrom?: 'spec' | 'previous-spec';
	rollback?: 'previous' | 'none' | (string & {});
	queryRegistry?: boolean;
}

export interface SwarmServiceScaleRequest {
	replicas: number;
}

export interface SwarmStackSummary {
	id: string;
	environmentId?: string;
	environmentName?: string;
	name: string;
	namespace: string;
	services: number;
	createdAt: string;
	updatedAt: string;
	clusterId?: string;
	hasSource: boolean;
}

export interface SwarmStackInspect {
	name: string;
	namespace: string;
	services: number;
	createdAt: string;
	updatedAt: string;
}

export interface SwarmStackRenderConfigRequest {
	name: string;
	composeContent: string;
	overrideContent?: string;
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
	overrideContent?: string;
	envContent?: string;
	files?: SwarmSyncFile[];
}

export interface SwarmStackDeployRequest {
	name: string;
	composeContent: string;
	overrideContent?: string;
	envContent?: string;
	files?: SwarmSyncFile[];
	withRegistryAuth?: boolean;
	prune?: boolean;
	resolveImage?: string;
}

export interface SwarmSyncFile {
	relativePath: string;
	content: string;
}

export interface SwarmStackDeployResponse {
	name: string;
}

export interface SwarmTaskSummary {
	id: string;
	environmentId?: string;
	environmentName?: string;
	name: string;
	serviceId: string;
	serviceName: string;
	nodeId: string;
	nodeName: string;
	desiredState: string;
	currentState: string;
	error?: string;
	containerId?: string;
	image?: string;
	slot?: number;
	createdAt: string;
	updatedAt: string;
}
