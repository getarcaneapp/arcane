import type { PaginatedWithCounts } from './base';
import type { ContainerRegistryCredential } from './containerregistry';
import type { ImageUpdateInfo } from './image';

export interface ContainerRestartPolicyCreate {
	name?: 'no' | 'always' | 'on-failure' | 'unless-stopped';
	maximumRetryCount?: number;
}

export interface ContainerPortBindingCreate {
	hostIp?: string;
	hostPort?: string;
}

export interface ContainerHostConfigCreate {
	binds?: string[];
	portBindings?: Record<string, ContainerPortBindingCreate[]>;
	restartPolicy?: ContainerRestartPolicyCreate;
	networkMode?: string;
	privileged?: boolean;
	autoRemove?: boolean;
	readonlyRootfs?: boolean;
	publishAllPorts?: boolean;
	memory?: number;
	memorySwap?: number;
	nanoCpus?: number;
	cpuShares?: number;
	capAdd?: string[];
	capDrop?: string[];
	mounts?: ContainerMountCreate[];
}

export interface ContainerEndpointSettingsCreate {
	aliases?: string[];
	ipv4Address?: string;
	ipv6Address?: string;
}

export interface ContainerMountCreate {
	type: string;
	source: string;
	target: string;
	readOnly?: boolean;
}

export interface ContainerHealthcheckCreate {
	test?: string[];
	interval?: number;
	timeout?: number;
	startPeriod?: number;
	startInterval?: number;
	retries?: number;
}

export interface ContainerNetworkingConfigCreate {
	endpointsConfig?: Record<string, ContainerEndpointSettingsCreate>;
}

export interface Container {
	name: string;
	image: string;
	command?: string[];
	cmd?: string[];
	entrypoint?: string[];
	workingDir?: string;
	user?: string;
	environment?: string[];
	env?: string[];
	labels?: Record<string, string>;
	exposedPorts?: Record<string, {}>;
	healthcheck?: ContainerHealthcheckCreate;
	hostConfig?: ContainerHostConfigCreate;
	networkingConfig?: ContainerNetworkingConfigCreate;
	hostname?: string;
	domainname?: string;
	attachStdout?: boolean;
	attachStderr?: boolean;
	attachStdin?: boolean;
	tty?: boolean;
	openStdin?: boolean;
	stdinOnce?: boolean;
	networkDisabled?: boolean;
	ports?: Record<string, string>;
	volumes?: string[];
	networks?: string[];
	restartPolicy?: string;
	privileged?: boolean;
	autoRemove?: boolean;
	memory?: number;
	cpus?: number;
	credentials?: ContainerRegistryCredential[];
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

export interface ContainerStatusCounts {
	runningContainers: number;
	stoppedContainers: number;
	totalContainers: number;
	unhealthyContainers: number;
	updatesAvailable: number;
}

export interface ContainerPort {
	ip?: string;
	privatePort: number;
	publicPort?: number;
	type: string;
}

export interface ContainerMount {
	type: string;
	name?: string;
	source?: string;
	destination: string;
	driver?: string;
	mode?: string;
	rw?: boolean;
	propagation?: string;
}

export interface ContainerNetworkEndpoint {
	ipamConfig?: unknown;
	links?: string[];
	aliases?: string[];
	macAddress?: string;
	driverOpts?: Record<string, string>;
	gwPriority?: number;
	networkId?: string;
	endpointId?: string;
	gateway?: string;
	ipAddress?: string;
	ipPrefixLen?: number;
	ipv6Gateway?: string;
	globalIPv6Address?: string;
	globalIPv6PrefixLen?: number;
	dnsNames?: string[];
}

export interface ContainerNetworkSettings {
	networks: Record<string, ContainerNetworkEndpoint>;
}

export interface ContainerState {
	status: string;
	running: boolean;
	startedAt?: string;
	finishedAt?: string;
	health?: ContainerHealth;
}

export interface ContainerHealthLogEntry {
	start?: string;
	end?: string;
	exitCode: number;
	output?: string;
}

export interface ContainerHealth {
	status: string;
	failingStreak: number;
	log?: ContainerHealthLogEntry[];
}

export interface ContainerHealthcheck {
	test?: string[];
	interval?: number;
	timeout?: number;
	startPeriod?: number;
	startInterval?: number;
	retries?: number;
}

export interface ContainerConfig {
	env?: string[];
	cmd?: string[];
	entrypoint?: string[];
	workingDir?: string;
	user?: string;
	hostname?: string;
	healthcheck?: ContainerHealthcheck;
}

export interface ContainerHostConfig {
	networkMode?: string;
	restartPolicy?: string;
	privileged?: boolean;
	autoRemove?: boolean;
	nanoCpus?: number;
	memory?: number;
}

export interface ContainerResourceSample {
	cpuPercent: number;
	memoryUsageBytes: number;
	memoryLimitBytes: number;
	sampleTime: string;
	blockReadBytes: number;
	blockWriteBytes: number;
	networkRxBytes: number;
	networkTxBytes: number;
	pids: number;
}

export interface ContainerResourceSampleSubscription {
	containerIds: string[];
}

export interface ContainerResourceSampleBatch {
	samples: Record<string, ContainerResourceSample>;
	error?: string;
	timestamp: string;
}

export interface ContainerSummary {
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
	updateStrategy?: string;
	ports: ContainerPort[];
	hostConfig: ContainerHostConfig;
	networkSettings: ContainerNetworkSettings;
	mounts: ContainerMount[];
	updateInfo?: ImageUpdateInfo;
	redeployDisabled?: boolean;
	/** Eligibility under the updater label and exclusion setting; absent on older agents. */
	autoUpdateEnabled?: boolean;
	hidden?: boolean;
	resourceSample?: ContainerResourceSample;
}

export interface ContainerComposeInfo {
	projectName: string;
	serviceName: string;
	workingDir?: string;
	configFiles?: string;
}

export interface ContainerSummaryGroup {
	groupName: string;
	items: ContainerSummary[];
}

export interface ContainerDetails {
	id: string;
	name: string;
	image: string;
	imageId: string;
	created: string;
	state: ContainerState;
	config: ContainerConfig;
	hostConfig: ContainerHostConfig;
	networkSettings: ContainerNetworkSettings;
	ports: ContainerPort[];
	mounts: ContainerMount[];
	labels?: Record<string, string>;
	composeInfo?: ContainerComposeInfo;
	iconLightUrl?: string;
	iconDarkUrl?: string;
	redeployDisabled?: boolean;
	/** Eligibility under the updater label and exclusion setting; absent on older agents. */
	autoUpdateEnabled?: boolean;
	/** Stored update check for the container's current policy; absent on older agents. */
	updateInfo?: ImageUpdateInfo;
}

export interface ContainerCreated {
	id: string;
	name: string;
	image: string;
	status: string;
	created: string;
}

export interface ContainerHostConfigEdit {
	binds?: string[];
	mounts?: ContainerMountCreate[];
	portBindings?: Record<string, ContainerPortBindingCreate[]>;
	restartPolicy?: ContainerRestartPolicyCreate;
	privileged?: boolean;
	capAdd?: string[];
	capDrop?: string[];
	autoRemove?: boolean;
	readonlyRootfs?: boolean;
	memory?: number;
	memorySwap?: number;
	nanoCpus?: number;
	cpuShares?: number;
}

export interface ContainerEdit {
	name?: string;
	image?: string;
	workingDir?: string;
	user?: string;
	hostname?: string;
	command?: string[];
	entrypoint?: string[];
	environment?: string[];
	labels?: Record<string, string>;
	healthcheck?: ContainerHealthcheckCreate;
	clearHealthcheck?: boolean;
	hostConfig?: ContainerHostConfigEdit;
	networkingConfig?: ContainerNetworkingConfigCreate;
}

export interface ContainerEditConfigNetwork {
	aliases?: string[];
	ipv4Address?: string;
	ipv6Address?: string;
}

export interface ContainerEditConfigHostConfig {
	binds?: string[];
	mounts?: ContainerMountCreate[];
	portBindings?: Record<string, ContainerPortBindingCreate[]>;
	restartPolicy: ContainerRestartPolicyCreate;
	networkMode?: string;
	privileged?: boolean;
	capAdd?: string[];
	capDrop?: string[];
	autoRemove?: boolean;
	readonlyRootfs?: boolean;
	memory?: number;
	memorySwap?: number;
	nanoCpus?: number;
	cpuShares?: number;
}

export interface ContainerEditConfig {
	id: string;
	name: string;
	image: string;
	command?: string[];
	entrypoint?: string[];
	workingDir?: string;
	user?: string;
	hostname?: string;
	environment?: string[];
	labels?: Record<string, string>;
	healthcheck?: ContainerHealthcheckCreate;
	hostConfig: ContainerEditConfigHostConfig;
	networks?: Record<string, ContainerEditConfigNetwork>;
	isCompose?: boolean;
	composeProject?: string;
	editDisabled?: boolean;
	running: boolean;
}

export interface ContainerProcesses {
	titles: string[];
	processes: string[][];
}

export type ContainerPaginatedResponse = PaginatedWithCounts<ContainerSummary, ContainerStatusCounts> & {
	groups?: ContainerSummaryGroup[];
	resourceSortSupported?: boolean;
};

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
		total_inactive_file?: number;
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
