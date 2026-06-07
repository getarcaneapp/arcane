export interface DockerInfo {
	success: boolean;
	apiVersion: string;
	gitCommit: string;
	goVersion: string;
	os: string;
	arch: string;
	buildTime: string;

	// System Info fields (from embedded system.Info)
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
