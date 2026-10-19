export const PODMAN_SYSTEMD_UNIT_LABEL = 'PODMAN_SYSTEMD_UNIT';

export type PodmanAutoUpdateMode = 'rest' | 'systemd' | 'unavailable';
export type PodmanSystemdScope = 'system' | 'user';
export type PodmanQuadletType = 'container' | 'pod' | 'kube' | 'network' | 'volume' | 'image' | 'build' | 'artifact';
export type PodmanKubeGenerateType = 'pod' | 'service' | 'deployment' | 'daemonset' | 'job';

export const PODMAN_QUADLET_TYPES: PodmanQuadletType[] = [
	'container',
	'pod',
	'kube',
	'network',
	'volume',
	'image',
	'build',
	'artifact'
];
export const PODMAN_KUBE_GENERATE_TYPES: PodmanKubeGenerateType[] = ['pod', 'service', 'deployment', 'daemonset', 'job'];

export interface PodmanStatus {
	detected: boolean;
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

export interface PodmanUnitStatus {
	name: string;
	description?: string;
	loadState: string;
	activeState: string;
	subState: string;
}

export interface PodmanInfo {
	host: {
		arch?: string;
		hostname?: string;
		kernel?: string;
		os?: string;
		cpus: number;
		memTotal: number;
		memFree: number;
		cgroupVersion?: string;
		networkBackend?: string;
		security: {
			rootless: boolean;
			selinuxEnabled: boolean;
			apparmorEnabled: boolean;
			seccompEnabled: boolean;
		};
	};
	store: {
		graphDriverName?: string;
		graphRoot?: string;
		containerStore: { number: number; paused: number; running: number; stopped: number };
		imageStore: { number: number };
	};
	version: {
		apiVersion?: string;
		version?: string;
		goVersion?: string;
		gitCommit?: string;
		builtTime?: string;
		osArch?: string;
	};
}

export interface PodmanPodContainer {
	id: string;
	names: string;
	status: string;
	restartCount: number;
}

export interface PodmanPod {
	id: string;
	name: string;
	namespace?: string;
	status: string;
	created: string;
	infraId?: string;
	cgroup?: string;
	labels?: Record<string, string>;
	networks?: string[];
	containers: PodmanPodContainer[];
}

export interface PodmanPodPortBinding {
	hostIp?: string;
	hostPort: string;
}

export interface PodmanPodInspectContainer {
	id: string;
	name: string;
	state: string;
}

export interface PodmanPodInspect {
	id: string;
	name: string;
	namespace?: string;
	created: string;
	state: string;
	hostname?: string;
	labels?: Record<string, string>;
	createInfra: boolean;
	infraContainerId?: string;
	infraConfig?: {
		portBindings?: Record<string, PodmanPodPortBinding[]>;
		hostNetwork: boolean;
		networks?: string[];
	};
	sharedNamespaces?: string[];
	numContainers: number;
	containers: PodmanPodInspectContainer[];
}

export interface PodmanPodPortMapping {
	containerPort: number;
	hostPort?: number;
	hostIp?: string;
	protocol?: string;
	range?: number;
}

export interface PodmanPodCreateRequest {
	name: string;
	labels?: Record<string, string>;
	hostname?: string;
	portMappings?: PodmanPodPortMapping[];
	network?: string;
	sharedNamespaces?: string[];
	noInfra?: boolean;
	advanced?: Record<string, unknown>;
}

export interface PodmanPodActionRequest {
	timeout?: number;
	signal?: string;
}

export interface PodmanPodCreateResponse {
	id: string;
}

export interface PodmanPodStats {
	pod: string;
	cid: string;
	name: string;
	cpu: string;
	memUsage: string;
	memUsageBytes: string;
	mem: string;
	netIo: string;
	blockIo: string;
	pids: string;
}

export interface PodmanPodTop {
	titles: string[];
	processes: string[][];
}

export interface PodmanPodPruneReport {
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

export interface PodmanQuadletFile {
	name: string;
	content: string;
}

export interface PodmanQuadletInstallRequest {
	name: string;
	content: string;
	extraFiles?: PodmanQuadletFile[];
	app?: string;
}

export interface PodmanQuadletUpdateRequest {
	content: string;
	extraFiles?: PodmanQuadletFile[];
}

export interface PodmanQuadletDetail {
	name: string;
	unitName: string;
	path: string;
	status: string;
	app?: string;
	type: PodmanQuadletType;
	template: boolean;
	unitStatus?: PodmanUnitStatus;
	containers?: PodmanContainer[];
}

export interface PodmanQuadletInstallReport {
	installedQuadlets: Record<string, string>;
	quadletErrors: Record<string, string>;
}

export interface PodmanQuadletRemoveReport {
	removed: string[];
	errors: Record<string, string>;
}

export interface PodmanKubePlayRequest {
	yaml: string;
	start?: boolean;
	replace?: boolean;
	networks?: string[];
}

export interface PodmanKubeDownRequest {
	yaml: string;
	force?: boolean;
}

export interface PodmanKubeGenerateRequest {
	names: string[];
	type?: PodmanKubeGenerateType;
	service?: boolean;
}

export interface PodmanKubeGenerateResponse {
	yaml: string;
}

export interface PodmanKubePlayReport {
	pods: { id: string; containers: string[]; initContainers: string[]; logs: string[]; containerErrors: string[] }[];
	volumes: { name: string }[];
	serviceContainerId?: string;
	exitCode?: number;
}

export interface PodmanKubeDownReport {
	stopReport: { id: string }[];
	rmReport: { id: string }[];
}

export interface PodmanSecret {
	id: string;
	createdAt: string;
	updatedAt: string;
	spec: {
		name: string;
		driver: { name: string; options?: Record<string, string> };
		labels?: Record<string, string>;
	};
}

export interface PodmanSecretCreateRequest {
	name: string;
	data: string;
	driver?: string;
	driverOptions?: Record<string, string>;
	labels?: Record<string, string>;
}

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
