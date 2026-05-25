// --- Swarm services, ports, mounts ---

export interface SwarmServicePort {
	protocol: string;
	targetPort: number;
	publishedPort?: number;
	publishMode?: string;
}

export interface RawSwarmServicePort {
	protocol?: string;
	Protocol?: string;
	targetPort?: number;
	TargetPort?: number;
	publishedPort?: number;
	PublishedPort?: number;
	publishMode?: string;
	PublishMode?: string;
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

export interface RawSwarmServiceMount {
	type?: string;
	Type?: string;
	source?: string;
	Source?: string;
	target?: string;
	Target?: string;
	readOnly?: boolean;
	ReadOnly?: boolean;
	volumeDriver?: string;
	VolumeDriver?: string;
	volumeOptions?: Record<string, string>;
	VolumeOptions?: Record<string, string>;
	devicePath?: string;
	DevicePath?: string;
}

export interface ServiceNetworkAttachment {
	target: string;
	aliases: string[];
}

export interface RawServiceNetworkAttachment {
	Target?: string;
	target?: string;
	Aliases?: string[];
	aliases?: string[];
}

export interface ServiceVirtualIP {
	networkID: string;
	addr: string;
}

export interface SwarmRuntimeStatus {
	enabled: boolean;
}

export interface RawServiceVirtualIP {
	NetworkID?: string;
	networkID?: string;
	Addr?: string;
	addr?: string;
}

export interface SwarmServiceSummary {
	id: string;
	name: string;
	image: string;
	mode: SwarmServiceModeName;
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

export type SwarmServiceModeName = 'replicated' | 'global' | 'replicated-job' | 'global-job' | 'unknown';

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

export interface SwarmServiceUpdateOptions {
	encodedRegistryAuth?: string;
	registryAuthFrom?: 'spec' | 'previous-spec';
	rollback?: 'previous' | 'none' | string;
	queryRegistry?: boolean;
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
	agent: SwarmNodeAgentStatus;
	address?: string | null;
	managerStatus?: string | null;
	reachability?: string | null;
	labels?: Record<string, string> | null;
	systemLabels?: Record<string, string> | null;
	engineVersion?: string | null;
	platform?: string | null;
	createdAt: string;
	updatedAt: string;
}

export type SwarmNodeAgentState = 'none' | 'pending' | 'offline' | 'connected' | 'mismatched';

export interface SwarmNodeAgentStatus {
	state: SwarmNodeAgentState;
	environmentId?: string | null;
	connected?: boolean | null;
	lastHeartbeat?: string | null;
	lastPollAt?: string | null;
	reportedNodeId?: string | null;
	reportedHostname?: string | null;
}

export interface SwarmNodeAgentDeployment {
	environmentId: string;
	agent: SwarmNodeAgentStatus;
	dockerRun: string;
	dockerCompose: string;
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

export interface SwarmStackSourceUpdateRequest {
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

// --- Compose projects ---

export interface ServicePort {
	mode?: string;
	target: number;
	published?: string;
	protocol?: string;
}

export interface ServiceVolume {
	type: string;
	source: string;
	target: string;
	read_only?: boolean;
	volume?: Record<string, any>;
	bind?: Record<string, any>;
}

export interface ProjectService {
	name?: string;
	image?: string;
	build?: string | Record<string, any>;
	container_name?: string;
	command?: string[] | string | null;
	entrypoint?: string[] | string | null;
	environment?: Record<string, string>;
	env_file?: string[];
	ports?: ServicePort[];
	volumes?: ServiceVolume[];
	networks?: Record<string, any>;
	restart?: string;
	depends_on?: Record<string, any>;
	labels?: Record<string, string>;
	healthcheck?: Record<string, any>;
	deploy?: Record<string, any>;
	[key: string]: any;
}

export interface IncludeFile {
	path: string;
	relativePath: string;
	content?: string;
}

export interface RuntimeService {
	name: string;
	image: string;
	status: string;
	containerId?: string;
	containerName?: string;
	ports?: string[];
	health?: string;
	iconUrl?: string;
	serviceConfig?: ProjectService;
	redeployDisabled?: boolean;
}

export interface ProjectUpdateInfo {
	status: 'has_update' | 'up_to_date' | 'unknown' | 'error';
	hasUpdate: boolean;
	imageCount: number;
	checkedImageCount: number;
	imagesWithUpdates: number;
	errorCount: number;
	errorMessage?: string;
	imageRefs?: string[];
	updatedImageRefs?: string[];
	lastCheckedAt?: string;
}

export interface Project {
	id: string;
	name: string;
	dirName?: string;
	relativePath?: string;
	path: string;
	iconUrl?: string;
	urls?: string[];
	runningCount: string;
	serviceCount: string;
	status: string;
	statusReason?: string;
	updatedAt: string;
	createdAt: string;
	isArchived?: boolean;
	isDiscovered?: boolean;
	archivedAt?: string;
	gitOpsManagedBy?: string;
	lastSyncCommit?: string;
	gitRepositoryURL?: string;
	hasBuildDirective?: boolean;
	redeployDisabled?: boolean;
	updateInfo?: ProjectUpdateInfo;
	services?: ProjectService[];
	runtimeServices?: RuntimeService[];
	composeContent?: string;
	composeFileName?: string;
	envContent?: string;
	includeFiles?: IncludeFile[];
	directoryFiles?: IncludeFile[];
}

export interface ProjectStatusCounts {
	runningProjects: number;
	stoppedProjects: number;
	totalProjects: number;
	archivedProjects: number;
}

// --- Compose templates ---

export interface TemplateRegistry {
	id: string;
	name: string;
	url: string;
	enabled: boolean;
	description: string;
	createdAt?: string;
	updatedAt?: string;
	lastFetchError?: string;
}

export interface Template {
	id: string;
	name: string;
	description: string;
	content: string;
	envContent?: string;
	isCustom: boolean;
	isRemote: boolean;
	registryId?: string;
	registry?: TemplateRegistry;
	metadata?: {
		version?: string;
		author?: string;
		tags?: string[];
		remoteUrl?: string;
		envUrl?: string;
		documentationUrl?: string;
		iconUrl?: string;
		updatedAt?: string;
	};
	createdAt: string;
	updatedAt: string;
}

export interface EnvVariable {
	key: string;
	value: string;
}

export interface TemplateContentData {
	template: Template;
	content: string;
	envContent: string;
	services: string[];
	envVariables: EnvVariable[];
}

export interface RemoteTemplate {
	id: string;
	name: string;
	description: string;
	version: string;
	author: string;
	tags: string[];
	compose_url: string;
	env_url?: string;
	documentation_url?: string;
}

export interface RemoteRegistry {
	$schema?: string;
	name: string;
	description: string;
	version: string;
	author: string;
	url: string;
	templates: RemoteTemplate[];
}

export interface TemplateRegistryConfig {
	url: string;
	name: string;
	enabled: boolean;
}
