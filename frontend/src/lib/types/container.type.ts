// Base Container Types

import type { ImageUpdateInfoDto } from './image.type';

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
}

// Container creation types
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
	memory?: number;
	memorySwap?: number;
	nanoCpus?: number;
	cpuShares?: number;
}

export interface NetworkingConfig {
	endpointsConfig?: Record<string, { aliases?: string[] }>;
}

export interface ContainerCreateRequest {
	name?: string;
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

// End Base Container Types

export interface ContainerStatusCounts {
	runningContainers: number;
	stoppedContainers: number;
	totalContainers: number;
}

export interface ContainerStateDto {
	status: string;
	running: boolean;
	startedAt: string;
	finishedAt: string;
	health?: {
		status: string;
		log?: Array<{
			start?: string;
			Start?: string;
			end?: string;
			End?: string;
			exitCode?: number;
			ExitCode?: number;
			output?: string;
			Output?: string;
		}>;
	};
}

export interface ContainerConfigDto {
	env?: string[];
	cmd?: string[];
	entrypoint?: string[];
	workingDir?: string;
	user?: string;
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
}

// Container Stats Types

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
}
