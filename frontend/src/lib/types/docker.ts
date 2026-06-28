import type { VulnerabilityScanSummary } from './environment';

// --- Container DTOs ---

export interface BaseContainer {
	id: string;
	names: string[];
	image: string;
	imageId: string;
	command: string;
	created: number;
	labels: Record<string, string>;
	state: string;
	status: string;
	iconLightUrl?: string;
	iconDarkUrl?: string;
}

export interface PortBinding {
	hostIp?: string;
	hostPort: string;
}

export interface RestartPolicy {
	name: 'no' | 'always' | 'on-failure' | 'unless-stopped';
	maximumRetryCount?: number;
}

export interface HostConfigCreate {
	binds?: string[];
	portBindings?: Record<string, PortBinding[]>;
	restartPolicy?: RestartPolicy;
	networkMode?: string;
	privileged?: boolean;
	autoRemove?: boolean;
	readonlyRootfs?: boolean;
	publishAllPorts?: boolean;
	memory?: number;
	memorySwap?: number;
	nanoCpus?: number;
	cpuShares?: number;
}

export interface NetworkingConfig {
	endpointsConfig?: Record<string, { aliases?: string[] }>;
}

export interface ContainerCreateRequest {
	name: string;
	image: string;
	cmd?: string[];
	entrypoint?: string[];
	env?: string[];
	exposedPorts?: Record<string, {}>;
	hostConfig?: HostConfigCreate;
	networkingConfig?: NetworkingConfig;
	labels?: Record<string, string>;
	workingDir?: string;
	user?: string;
	hostname?: string;
	domainname?: string;
	attachStdout?: boolean;
	attachStderr?: boolean;
	attachStdin?: boolean;
	networkDisabled?: boolean;
	tty?: boolean;
	openStdin?: boolean;
	stdinOnce?: boolean;
}

export interface ContainerSummaryDto extends BaseContainer {
	ports: ContainerPorts[];
	hostConfig: ContainerHostConfig;
	networkSettings: ContainerNetworkSettings;
	mounts: ContainerMounts[];
	updateInfo?: ImageUpdateInfoDto;
	redeployDisabled?: boolean;
}

export interface ContainerSummaryGroupDto {
	groupName: string;
	items: ContainerSummaryDto[];
}

export interface ContainerPorts {
	ip?: string;
	privatePort: number;
	publicPort?: number;
	type: string;
}

export interface ContainerHostConfig {
	networkMode: string;
	restartPolicy?: string;
	privileged?: boolean;
	autoRemove?: boolean;
	nanoCpus?: number;
	memory?: number;
}

export interface ContainerNetworkSettings {
	networks: Record<string, ContainerNetwork>;
}

export interface ContainerMounts {
	type: string;
	name?: string;
	source?: string;
	destination: string;
	driver?: string;
	mode?: string;
	rw?: boolean;
	propagation?: string;
}

export interface ContainerNetwork {
	ipamConfig: any | null;
	links: string[] | null;
	aliases: string[] | null;
	macAddress: string;
	driverOpts: Record<string, string> | null;
	gwPriority: number;
	networkId: string;
	endpointId: string;
	gateway: string;
	ipAddress: string;
	ipPrefixLen: number;
	ipv6Gateway: string;
	globalIPv6Address: string;
	globalIPv6PrefixLen: number;
	dnsNames: string[] | null;
}

export interface ContainerStatusCounts {
	runningContainers: number;
	stoppedContainers: number;
	totalContainers: number;
}

export interface ContainerHealthLogEntry {
	start?: string;
	end?: string;
	exitCode?: number;
	output?: string;
}

export interface ContainerHealthDto {
	status: string;
	failingStreak?: number;
	log?: ContainerHealthLogEntry[];
}

export interface ContainerStateDto {
	status: string;
	running: boolean;
	startedAt: string;
	finishedAt: string;
	health?: ContainerHealthDto;
}

export interface ContainerHealthcheckDto {
	test?: string[];
	interval?: number;
	timeout?: number;
	startPeriod?: number;
	startInterval?: number;
	retries?: number;
}

export interface ContainerConfigDto {
	env?: string[];
	cmd?: string[];
	entrypoint?: string[];
	workingDir?: string;
	user?: string;
	healthcheck?: ContainerHealthcheckDto;
}

export interface ComposeInfo {
	projectName: string;
	serviceName: string;
	workingDir?: string;
	configFiles?: string;
}

export interface ContainerDetailsDto {
	id: string;
	name: string;
	image: string;
	imageId: string;
	created: string;
	state: ContainerStateDto;
	config: ContainerConfigDto;
	hostConfig: ContainerHostConfig;
	networkSettings: ContainerNetworkSettings;
	ports: ContainerPorts[];
	mounts: ContainerMounts[];
	labels: Record<string, string>;
	composeInfo?: ComposeInfo;
	iconLightUrl?: string;
	iconDarkUrl?: string;
	redeployDisabled?: boolean;
}

export interface ContainerCommitRequest {
	repository?: string;
	tag?: string;
	comment?: string;
	author?: string;
	changes?: string[];
	noPause?: boolean;
}

export interface ContainerCommitResult {
	id: string;
}

// --- Container stats ---

export interface BlkioStatEntry {
	major: number;
	minor: number;
	op: string;
	value: number;
}

export interface BlkioStats {
	io_merged_recursive: BlkioStatEntry[] | null;
	io_queue_recursive: BlkioStatEntry[] | null;
	io_service_bytes_recursive: BlkioStatEntry[] | null;
	io_service_time_recursive: BlkioStatEntry[] | null;
	io_serviced_recursive: BlkioStatEntry[] | null;
	io_time_recursive: BlkioStatEntry[] | null;
	io_wait_time_recursive: BlkioStatEntry[] | null;
	sectors_recursive: BlkioStatEntry[] | null;
}

export interface ThrottlingData {
	periods: number;
	throttled_periods: number;
	throttled_time: number;
}

export interface CPUUsage {
	total_usage: number;
	usage_in_kernelmode: number;
	usage_in_usermode: number;
	percpu_usage?: number[];
}

export interface CPUStats {
	cpu_usage: CPUUsage;
	online_cpus: number;
	system_cpu_usage: number;
	throttling_data: ThrottlingData;
}

export interface MemoryStats {
	limit: number;
	usage: number;
	max_usage?: number;
	stats?: {
		active_anon?: number;
		active_file?: number;
		anon?: number;
		anon_thp?: number;
		file?: number;
		file_dirty?: number;
		file_mapped?: number;
		file_writeback?: number;
		inactive_anon?: number;
		inactive_file?: number;
		kernel_stack?: number;
		pgactivate?: number;
		pgdeactivate?: number;
		pgfault?: number;
		pglazyfree?: number;
		pglazyfreed?: number;
		pgmajfault?: number;
		pgrefill?: number;
		pgscan?: number;
		pgsteal?: number;
		shmem?: number;
		slab?: number;
		slab_reclaimable?: number;
		slab_unreclaimable?: number;
		sock?: number;
		thp_collapse_alloc?: number;
		thp_fault_alloc?: number;
		unevictable?: number;
		workingset_activate?: number;
		workingset_nodereclaim?: number;
		workingset_refault?: number;
		[key: string]: number | undefined;
	};
	failcnt?: number;
}

export interface NetworkStats {
	rx_bytes: number;
	rx_dropped: number;
	rx_errors: number;
	rx_packets: number;
	tx_bytes: number;
	tx_dropped: number;
	tx_errors: number;
	tx_packets: number;
}

export interface PidsStats {
	current: number;
	limit: number;
}

export interface StorageStats {
	read_count_normalized?: number;
	read_size_bytes?: number;
	write_count_normalized?: number;
	write_size_bytes?: number;
}

export interface ContainerStatsHistorySample {
	cpuTenths: number;
	memoryTenths: number;
	memoryUsageBytes: number;
}

export interface ContainerStats {
	id: string;
	name: string;
	read: string;
	preread: string;
	num_procs: number;
	pids_stats: PidsStats;
	blkio_stats: BlkioStats;
	cpu_stats: CPUStats;
	precpu_stats: CPUStats;
	memory_stats: MemoryStats;
	networks: Record<string, NetworkStats>;
	storage_stats: StorageStats;
	statsHistory?: ContainerStatsHistorySample[];
	currentHistorySample?: ContainerStatsHistorySample;
}

// --- Image DTOs ---

export interface ImageUpdateInfoDto {
	hasUpdate: boolean;
	updateType: string;
	currentVersion: string;
	latestVersion: string;
	currentDigest: string;
	latestDigest: string;
	checkTime: string;
	responseTimeMs: number;
	error: string;
	authMethod?: 'none' | 'anonymous' | 'credential' | 'unknown';
	authUsername?: string;
	authRegistry?: string;
	usedCredential?: boolean;
	activityId?: string;
}

export interface ImageUsageCounts {
	imagesInuse: number;
	imagesUnused: number;
	totalImages: number;
	totalImageSize: number;
}

export interface ImageUsedByDto {
	type: 'project' | 'container';
	name: string;
	id?: string;
}

export interface ImageSummaryDto {
	id: string;
	repoTags: string[];
	repoDigests: string[];
	created: number;
	size: number;
	virtualSize: number;
	labels: Record<string, unknown> | null;
	inUse: boolean;
	usedBy?: ImageUsedByDto[];
	repo: string;
	tag: string;
	updateInfo?: ImageUpdateInfoDto;
	vulnerabilityScan?: VulnerabilityScanSummary;
}

export interface ImageDetailSummaryDto {
	id: string;
	repoTags: string[];
	repoDigests: string[];
	parent: string;
	comment: string;
	created: string;
	dockerVersion: string;
	author: string;
	config: {
		exposedPorts?: Record<string, unknown>;
		env?: string[];
		cmd?: string[];
		volumes?: Record<string, unknown>;
		workingDir?: string;
		argsEscaped?: boolean;
	};
	architecture: string;
	os: string;
	size: number;
	graphDriver: {
		data: unknown | null;
		name: string;
	};
	rootFs: {
		type: string;
		layers: string[];
	};
	metadata: {
		lastTagTime: string;
	};
	descriptor: {
		mediaType: string;
		digest: string;
		size: number;
	};
}

export interface ImageTagRequest {
	repository: string;
	tag?: string;
}

export interface ImageHistoryItemDto {
	id: string;
	created: number;
	createdBy: string;
	tags: string[];
	size: number;
	comment: string;
}

export interface ImageSearchResultDto {
	name: string;
	description: string;
	starCount: number;
	official: boolean;
	automated: boolean;
}

export interface ImageAttestationSubjectDto {
	name: string;
	digest: Record<string, string>;
}

export interface ImageAttestationDto {
	digest: string;
	mediaType: string;
	artifactType?: string;
	predicateType: string;
	statementType?: string;
	subject: ImageAttestationSubjectDto[];
	platform?: string;
	size: number;
	statement?: unknown;
}

export interface ImageAttestationListDto {
	imageRef: string;
	subjectDigest: string;
	platform?: string;
	attestations: ImageAttestationDto[];
}

export interface ImageAttestationRequestOptions {
	platform?: string;
	predicateType?: string;
	statement?: boolean;
}

export type ImageUpdateData = ImageUpdateInfoDto;

export type ImageBuildStatus = 'running' | 'success' | 'failed';

export interface ImageBuildRecord {
	id: string;
	environmentId: string;
	userId?: string;
	username?: string;
	status: ImageBuildStatus;
	provider?: string;
	contextDir: string;
	dockerfile?: string;
	target?: string;
	tags?: string[];
	platforms?: string[];
	buildArgs?: Record<string, string>;
	labels?: Record<string, string>;
	cacheFrom?: string[];
	cacheTo?: string[];
	noCache: boolean;
	pull: boolean;
	network?: string;
	isolation?: string;
	shmSize?: number;
	ulimits?: Record<string, string>;
	entitlements?: string[];
	privileged: boolean;
	extraHosts?: string[];
	push: boolean;
	load: boolean;
	digest?: string;
	errorMessage?: string;
	output?: string;
	outputTruncated: boolean;
	completedAt?: string;
	durationMs?: number;
	createdAt: string;
}

// --- Networks ---

export interface IPAMConfig {
	subnet?: string;
	gateway?: string;
	ipRange?: string;
	auxAddress?: Record<string, string>;
}

export interface IPAM {
	driver?: string;
	options?: Record<string, string>;
	config?: IPAMConfig[];
}

export interface NetworkCreateOptions {
	driver?: string;
	checkDuplicate?: boolean;
	internal?: boolean;
	attachable?: boolean;
	ingress?: boolean;
	ipam?: IPAM;
	enableIPv6?: boolean;
	options?: Record<string, string>;
	labels?: Record<string, string>;
}

export interface NetworkCreateRequest {
	name: string;
	options: NetworkCreateOptions;
}

export interface NetworkUsageCounts {
	inuse: number;
	unused: number;
	total: number;
}

export interface ContainerEndpointDto {
	id?: string;
	name: string;
	endpointId: string;
	macAddress: string;
	ipv4Address: string;
	ipv6Address: string;
}

export interface IPAMSubnetDto {
	subnet: string;
	gateway?: string;
	ipRange?: string;
	auxAddress?: Record<string, string>;
}

export interface IPAMDto {
	driver: string;
	options?: Record<string, string>;
	config?: IPAMSubnetDto[];
}

export interface NetworkSummaryDto {
	id: string;
	name: string;
	driver: string;
	scope: string;
	created: string;
	options?: Record<string, string> | null;
	labels?: Record<string, string> | null;
	inUse: boolean;
	isDefault?: boolean;
}

export interface ConfigReference {
	Network?: string;
}

export interface PeerInfo {
	Name?: string;
	IP?: string;
}

export interface Task {
	Name?: string;
	EndpointID?: string;
	EndpointIP?: string;
	Info?: Record<string, string>;
}

export interface ServiceInfo {
	VIP?: string;
	Ports?: string[];
	LocalLBIndex?: number;
	Tasks?: Task[];
}

export interface NetworkInspectDto {
	id: string;
	name: string;
	driver: string;
	scope: string;
	created: string;
	options?: Record<string, string> | null;
	labels?: Record<string, string> | null;
	containers?: Record<string, ContainerEndpointDto> | null;
	containersList?: ContainerEndpointDto[];
	ipam?: IPAMDto;
	internal: boolean;
	attachable: boolean;
	ingress: boolean;
	enableIPv6?: boolean;
	enableIPv4?: boolean;
	configFrom?: ConfigReference;
	configOnly?: boolean;
	peers?: PeerInfo[];
	services?: Record<string, ServiceInfo>;
}

export type TopologyNodeType = 'network' | 'container';

export interface TopologyNodeMetadata {
	driver?: string;
	scope?: string;
	status?: string;
	image?: string;
	isDefault?: boolean;
}

export interface TopologyNodeDto {
	id: string;
	name: string;
	type: TopologyNodeType;
	metadata: TopologyNodeMetadata;
}

export interface TopologyEdgeDto {
	id: string;
	source: string;
	target: string;
	ipv4Address?: string;
	ipv6Address?: string;
}

export interface NetworkTopologyDto {
	nodes: TopologyNodeDto[];
	edges: TopologyEdgeDto[];
}

// --- Volumes ---

export interface VolumeUsageData {
	size: number;
	refCount: number;
}

export interface VolumeCreateRequest {
	name: string;
	driver?: string;
	driverOpts?: Record<string, string>;
	labels?: Record<string, string>;
}

export interface VolumeSummaryDto {
	id: string;
	name: string;
	driver: string;
	mountpoint: string;
	scope: string;
	options: Record<string, string> | null;
	labels: Record<string, string>;
	createdAt: string;
	inUse: boolean;
	usageData?: VolumeUsageData;
	size: number;
	activityId?: string;
}

export interface VolumeDetailDto extends VolumeSummaryDto {
	containers: string[];
}

export interface VolumeUsageDto {
	inUse: boolean;
	containers: string[];
}

export interface VolumeUsageCounts {
	inuse: number;
	unused: number;
	total: number;
}

export interface VolumeSizeInfo {
	name: string;
	size: number;
	refCount: number;
}

// --- Ports ---

export interface PortMappingDto {
	id: string;
	containerId: string;
	containerName: string;
	hostIp?: string;
	hostPort?: number;
	containerPort: number;
	protocol: string;
	isPublished: boolean;
}

// --- Docker engine info ---

export interface DockerInfo {
	success: boolean;
	apiVersion: string;
	gitCommit: string;
	goVersion: string;
	os: string;
	arch: string;
	buildTime: string;

	ID: string;
	Containers: number;
	ContainersRunning: number;
	ContainersPaused: number;
	ContainersStopped: number;
	Images: number;
	Driver: string;
	DriverStatus: string[][];
	SystemStatus?: string[][];
	Plugins: PluginsInfo;
	MemoryLimit: boolean;
	SwapLimit: boolean;
	KernelMemory?: boolean;
	KernelMemoryTCP?: boolean;
	CpuCfsPeriod: boolean;
	CpuCfsQuota: boolean;
	CPUShares: boolean;
	CPUSet: boolean;
	PidsLimit: boolean;
	IPv4Forwarding: boolean;
	Debug: boolean;
	NFd: number;
	OomKillDisable: boolean;
	NGoroutines: number;
	SystemTime: string;
	LoggingDriver: string;
	CgroupDriver: string;
	CgroupVersion?: string;
	NEventsListener: number;
	KernelVersion: string;
	OperatingSystem: string;
	OSVersion: string;
	OSType: string;
	Architecture: string;
	IndexServerAddress: string;
	RegistryConfig: any;
	NCPU: number;
	MemTotal: number;
	GenericResources: any[];
	DockerRootDir: string;
	HttpProxy: string;
	HttpsProxy: string;
	NoProxy: string;
	Name: string;
	Labels: string[];
	ExperimentalBuild: boolean;
	ServerVersion: string;
	Runtimes: Record<string, RuntimeWithStatus>;
	DefaultRuntime: string;
	Swarm: any;
	LiveRestoreEnabled: boolean;
	Isolation: string;
	InitBinary: string;
	ContainerdCommit: Commit;
	RuncCommit: Commit;
	InitCommit: Commit;
	SecurityOptions: string[];
	ProductLicense?: string;
	DefaultAddressPools?: any[];
	FirewallBackend?: any;
	CDISpecDirs: string[];
	DiscoveredDevices?: any[];
	Containerd?: any;
	Warnings: string[];
}

export interface PluginsInfo {
	Volume?: string[];
	Network?: string[];
	Authorization?: string[];
	Log?: string[];
}

export interface RuntimeWithStatus {
	path: string;
	runtimeArgs?: string[];
	status?: Record<string, string>;
}

export interface Commit {
	ID: string;
	Expected: string;
}

// --- Container registries ---

export type RegistryType = 'generic' | 'ecr';

export interface ContainerRegistryCreateDto {
	url: string;
	username?: string;
	token?: string;
	description?: string;
	insecure?: boolean;
	enabled?: boolean;
	registryType?: RegistryType;
	awsAccessKeyId?: string;
	awsSecretAccessKey?: string;
	awsRegion?: string;
}

export interface ContainerRegistryUpdateDto {
	url?: string;
	username?: string;
	token?: string;
	description?: string;
	insecure?: boolean;
	enabled?: boolean;
	registryType?: RegistryType;
	awsAccessKeyId?: string;
	awsSecretAccessKey?: string;
	awsRegion?: string;
}

export interface ContainerRegistry {
	id: string;
	url: string;
	username: string;
	token: string;
	description?: string;
	insecure?: boolean;
	enabled?: boolean;
	registryType?: RegistryType;
	awsAccessKeyId?: string;
	awsRegion?: string;
	createdAt?: string;
	updatedAt?: string;
}

export interface ContainerRegistryPullUsageResponse {
	registries: ContainerRegistryPullUsage[];
}

export interface ContainerRegistryPullUsage {
	registryId: string;
	provider: string;
	registry: string;
	displayName: string;
	repository?: string;
	limit?: number;
	remaining?: number;
	used?: number;
	windowSeconds?: number;
	observedPulls: number;
	authMethod: 'anonymous' | 'credential' | 'unknown';
	authUsername?: string;
	source?: string;
	checkedAt: string;
	error?: string;
}
