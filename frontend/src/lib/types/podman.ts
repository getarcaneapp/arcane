export const PODMAN_SYSTEMD_UNIT_LABEL = 'PODMAN_SYSTEMD_UNIT';

export type PodmanAutoUpdateMode = 'rest' | 'systemd' | 'unavailable';
export type PodmanSystemdScope = 'system' | 'user';
export type QuadletType = 'container' | 'pod' | 'kube' | 'network' | 'volume' | 'image' | 'build' | 'artifact';
export type KubeGenerateType = 'pod' | 'service' | 'deployment' | 'daemonset' | 'job';

export const PODMAN_QUADLET_TYPES: QuadletType[] = [
	'container',
	'pod',
	'kube',
	'network',
	'volume',
	'image',
	'build',
	'artifact'
];
export const PODMAN_KUBE_GENERATE_TYPES: KubeGenerateType[] = ['pod', 'service', 'deployment', 'daemonset', 'job'];

export interface PodmanStatus {
	detected: boolean;
	podCount: number;
	version?: string;
	apiVersion?: string;
	rootless: boolean;
	quadletsSupported: boolean;
	quadletAppsSupported: boolean;
	autoUpdateMode: PodmanAutoUpdateMode;
	systemdAvailable: boolean;
	systemdScope?: PodmanSystemdScope;
	systemdReason?: string;
}

export interface PodmanInfoIDMap {
	container_id: number;
	host_id: number;
	size: number;
}

export interface PodmanInfoHelperBinary {
	executable?: string;
	package?: string;
	version?: string;
}

export interface PodmanInfoHost {
	arch?: string;
	buildahVersion?: string;
	cgroupManager?: string;
	cgroupVersion?: string;
	cgroupControllers?: string[];
	conmon: { package?: string; path?: string; version?: string };
	cpus: number;
	databaseBackend?: string;
	distribution: { distribution?: string; variant?: string; version?: string; codename?: string };
	eventLogger?: string;
	hostname?: string;
	idMappings: { uidmap?: PodmanInfoIDMap[]; gidmap?: PodmanInfoIDMap[] };
	kernel?: string;
	logDriver?: string;
	memFree: number;
	memTotal: number;
	networkBackend?: string;
	networkBackendInfo: {
		backend?: string;
		version?: string;
		package?: string;
		path?: string;
		dns: { version?: string; package?: string; path?: string };
		defaultNetwork?: string;
	};
	ociRuntime: { name?: string; package?: string; path?: string; version?: string };
	os?: string;
	remoteSocket: { path?: string; exists: boolean };
	rootlessNetworkCmd?: string;
	serviceIsRemote: boolean;
	security: {
		rootless: boolean;
		selinuxEnabled: boolean;
		apparmorEnabled: boolean;
		seccompEnabled: boolean;
		seccompProfilePath?: string;
		capabilities?: string;
	};
	slirp4netns: PodmanInfoHelperBinary;
	pasta: PodmanInfoHelperBinary;
	swapFree: number;
	swapTotal: number;
	uptime?: string;
}

export interface PodmanInfoStore {
	configFile?: string;
	containerStore: { number: number; paused: number; running: number; stopped: number };
	graphDriverName?: string;
	graphRoot?: string;
	graphStatus?: Record<string, string>;
	imageStore: { number: number };
	runRoot?: string;
	volumePath?: string;
}

export interface PodmanInfo {
	host: PodmanInfoHost;
	store: PodmanInfoStore;
	registries: { search?: string[] };
	plugins: { network?: string[]; volume?: string[]; log?: string[] };
	version: {
		apiVersion?: string;
		version?: string;
		goVersion?: string;
		gitCommit?: string;
		builtTime?: string;
		osArch?: string;
	};
}

export interface PodmanUnitStatus {
	name: string;
	description?: string;
	loadState: string;
	activeState: string;
	subState: string;
}

export interface PodContainer {
	id: string;
	names: string;
	status: string;
	restartCount: number;
}

export interface Pod {
	id: string;
	name: string;
	namespace?: string;
	status: string;
	created: string;
	infraId?: string;
	cgroup?: string;
	labels?: Record<string, string>;
	networks?: string[];
	containers: PodContainer[] | null;
}

export interface PodPortBinding {
	hostIp?: string;
	hostPort: string;
}

export interface PodInfraConfig {
	portBindings?: Record<string, PodPortBinding[]>;
	hostNetwork: boolean;
	staticIp?: string;
	staticMac?: string;
	noManageResolvConf: boolean;
	dnsServer?: string[];
	dnsSearch?: string[];
	dnsOption?: string[];
	noManageHostname: boolean;
	noManageHosts: boolean;
	hostAdd?: string[];
	hostsFile?: string;
	networks?: string[];
	networkOptions?: Record<string, string[]>;
	pidNs?: string;
	userNs?: string;
	utsNs?: string;
}

export interface PodInspectContainer {
	id: string;
	name: string;
	state: string;
	image?: string;
	status?: string;
	created: string;
	exitCode: number;
	isInfra: boolean;
	ports?: PodPortMapping[];
}

export interface PodMount {
	type: string;
	name?: string;
	source?: string;
	destination: string;
	driver?: string;
	mode?: string;
	options?: string[];
	rw: boolean;
	propagation?: string;
	subPath?: string;
}

export interface PodDevice {
	pathOnHost: string;
	pathInContainer: string;
	cgroupPermissions?: string;
}

export interface PodWeightDevice {
	path: string;
	weight: number;
}

export interface PodThrottleDevice {
	path: string;
	rate: number;
}

export interface PodInspect {
	id: string;
	name: string;
	namespace?: string;
	created: string;
	state: string;
	createCommand?: string[];
	exitPolicy?: string;
	restartPolicy?: string;
	hostname?: string;
	labels?: Record<string, string>;
	createCgroup: boolean;
	cgroupParent?: string;
	cgroupPath?: string;
	createInfra: boolean;
	infraContainerId?: string;
	infraConfig?: PodInfraConfig;
	sharedNamespaces?: string[];
	numContainers: number;
	containers: PodInspectContainer[] | null;
	cpuPeriod?: number;
	cpuQuota?: number;
	cpuShares?: number;
	cpusetCpus?: string;
	cpusetMems?: string;
	memoryLimit?: number;
	memorySwap?: number;
	blkioWeight?: number;
	blkioWeightDevices?: PodWeightDevice[];
	deviceReadBps?: PodThrottleDevice[];
	deviceWriteBps?: PodThrottleDevice[];
	mounts?: PodMount[];
	devices?: PodDevice[];
	volumesFrom?: string[];
	securityOpts?: string[];
}

export interface PodPortMapping {
	containerPort: number;
	hostPort?: number;
	hostIp?: string;
	protocol?: string;
	range?: number;
}

export interface PodNetwork {
	name: string;
	aliases?: string[];
	staticIps?: string[];
	staticMac?: string;
	interfaceName?: string;
}

export interface PodVolume {
	source: string;
	destination: string;
	options?: string[];
}

export interface PodDeviceLimit {
	device: string;
	value: string;
}

export type PodmanPodExitPolicy = 'continue' | 'stop';
export type PodmanPodRestartPolicy = 'no' | 'always' | 'on-failure' | 'unless-stopped';

export interface PodCreateRequest {
	name: string;
	hostname?: string;
	labels?: Record<string, string>;
	exitPolicy?: PodmanPodExitPolicy;
	restartPolicy?: PodmanPodRestartPolicy;
	restartRetries?: number;
	noInfra?: boolean;
	infraImage?: string;
	infraCommand?: string[];
	infraName?: string;
	infraConmonPidFile?: string;
	sharedNamespaces?: string[];
	shareParent?: boolean;
	pid?: string;
	ipc?: string;
	uts?: string;
	userns?: string;
	uidMap?: string[];
	gidMap?: string[];
	networkMode?: string;
	networkOptions?: string[];
	networks?: PodNetwork[];
	portMappings?: PodPortMapping[];
	dnsServers?: string[];
	dnsSearch?: string[];
	dnsOptions?: string[];
	noManageResolvConf?: boolean;
	addHosts?: string[];
	noHosts?: boolean;
	noHostname?: boolean;
	hostsFile?: string;
	cgroupParent?: string;
	cpus?: number;
	cpuShares?: number;
	cpusetCpus?: string;
	cpusetMems?: string;
	memory?: string;
	memorySwap?: string;
	blkioWeight?: number;
	blkioWeightDevices?: PodDeviceLimit[];
	deviceReadBps?: PodDeviceLimit[];
	deviceWriteBps?: PodDeviceLimit[];
	devices?: string[];
	gpus?: string[];
	volumes?: PodVolume[];
	volumesFrom?: string[];
	shmSize?: string;
	shmSizeSystemd?: string;
	securityOpt?: string[];
	sysctl?: Record<string, string>;
}

export interface PodActionRequest {
	timeout?: number;
	signal?: string;
}

export interface PodCreateResponse {
	id: string;
}

export interface PodTop {
	titles: string[] | null;
	processes: string[][] | null;
}

export interface PodPruneReport {
	id: string;
}

export interface PodmanContainer {
	id: string;
	names: string[];
	image: string;
	imageId?: string;
	state: string;
	status?: string;
	created: string;
	startedAt?: number;
	exitCode: number;
	exited: boolean;
	labels?: Record<string, string>;
	pod?: string;
	podName?: string;
	isInfra: boolean;
}

export interface QuadletFile {
	name: string;
	content: string;
}

export interface QuadletInstallRequest {
	name: string;
	content: string;
	extraFiles?: QuadletFile[];
	app?: string;
}

export interface QuadletUpdateRequest {
	content: string;
	extraFiles?: QuadletFile[];
}

export interface QuadletDetail {
	name: string;
	unitName: string;
	path: string;
	status: string;
	app?: string;
	type: QuadletType;
	template: boolean;
	unitStatus?: PodmanUnitStatus;
	containers?: PodmanContainer[];
}

export interface QuadletInstallReport {
	installedQuadlets: Record<string, string> | null;
	quadletErrors: Record<string, string> | null;
}

export interface QuadletRemoveReport {
	removed: string[] | null;
	errors: Record<string, string> | null;
}

export interface KubePlayRequest {
	yaml: string;
	start?: boolean;
	replace?: boolean;
	networks?: string[];
}

export interface KubeDownRequest {
	yaml: string;
	force?: boolean;
}

export interface KubeGenerateRequest {
	names: string[];
	type?: KubeGenerateType;
	service?: boolean;
}

export interface KubeGenerateResponse {
	yaml: string;
}

export interface KubePlayPod {
	id: string;
	containers: string[] | null;
	initContainers: string[] | null;
	logs: string[] | null;
	containerErrors: string[] | null;
}

export interface KubePlayVolume {
	name: string;
}

export interface KubePlayReport {
	pods: KubePlayPod[] | null;
	volumes: KubePlayVolume[] | null;
	serviceContainerId?: string;
	exitCode?: number;
}

export interface KubeDownPod {
	id: string;
}

export interface KubeDownReport {
	stopReport: KubeDownPod[] | null;
	rmReport: KubeDownPod[] | null;
}

export interface PodmanSecretDriver {
	name: string;
	options?: Record<string, string>;
}

export interface PodmanSecretSpec {
	name: string;
	driver: PodmanSecretDriver;
	labels?: Record<string, string>;
}

export interface PodmanSecret {
	id: string;
	createdAt: string;
	updatedAt: string;
	spec: PodmanSecretSpec;
}

export interface PodmanSecretCreateRequest {
	name: string;
	data: string;
	driver?: string;
	driverOptions?: Record<string, string>;
	labels?: Record<string, string>;
}

export type PodmanSecretUpdateRequest = Omit<PodmanSecretCreateRequest, 'name'>;

export interface PodmanSecretCreateResponse {
	id: string;
}

export interface PodmanAutoUpdateOptions {
	dryRun?: boolean;
	rollback?: boolean;
	tlsVerify?: boolean;
	authfile?: string;
	filters?: Record<string, string[]>;
}

export interface PodmanAutoUpdateReport {
	containerId: string;
	containerName: string;
	imageName: string;
	policy: string;
	systemdUnit: string;
	updated: string;
}

export interface PodmanAutoUpdateResult {
	mode: PodmanAutoUpdateMode;
	reports: PodmanAutoUpdateReport[] | null;
	errors: string[] | null;
	unit?: PodmanUnitStatus;
}
