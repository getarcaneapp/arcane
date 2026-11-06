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
	RegistryConfig: RegistryServiceConfig | null;
	NCPU: number;
	MemTotal: number;
	GenericResources: GenericResource[] | null;
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
	Swarm: SwarmInfo;
	LiveRestoreEnabled: boolean;
	Isolation: string;
	InitBinary: string;
	ContainerdCommit: Commit;
	RuncCommit: Commit;
	InitCommit: Commit;
	SecurityOptions: string[];
	ProductLicense?: string;
	DefaultAddressPools?: NetworkAddressPool[];
	FirewallBackend?: FirewallInfo;
	CDISpecDirs: string[];
	DiscoveredDevices?: DeviceInfo[];
	Containerd?: ContainerdInfo;
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

export interface RegistryIndexInfo {
	Name: string;
	Mirrors: string[];
	Secure: boolean;
	Official: boolean;
}

export interface RegistryServiceConfig {
	InsecureRegistryCIDRs: string[];
	IndexConfigs: Record<string, RegistryIndexInfo>;
	Mirrors: string[];
}

export interface GenericResource {
	NamedResourceSpec?: { Kind: string; Value: string };
	DiscreteResourceSpec?: { Kind: string; Value: number };
}

export interface SwarmInfo {
	NodeID?: string;
	NodeAddr?: string;
	LocalNodeState?: string;
	ControlAvailable?: boolean;
	Error?: string;
	Nodes?: number;
	Managers?: number;
}

export interface NetworkAddressPool {
	Base: string;
	Size: number;
}

export interface FirewallInfo {
	Driver: string;
	Info?: string[][];
}

export interface DeviceInfo {
	Source: string;
	ID: string;
}

export interface ContainerdInfo {
	Address?: string;
	Namespaces?: { Containers: string; Plugins: string };
}
