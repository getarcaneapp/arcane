export interface RuntimeInfo {
	goroutines: number;
	wsWorkerGoroutines: number;
	leakedGoroutines: number;
	leakScannedAt?: string;
	gomaxprocs: number;
	numCpu: number;
	goVersion: string;
	os: string;
	arch: string;
	numCgoCall: number;
	uptimeSeconds: number;
}

export interface MemoryInfo {
	alloc: number;
	totalAlloc: number;
	sys: number;
	heapAlloc: number;
	heapSys: number;
	heapInuse: number;
	heapIdle: number;
	heapReleased: number;
	heapObjects: number;
	stackInuse: number;
	stackSys: number;
	mspanInuse: number;
	mcacheInuse: number;
	nextGc: number;
	numGc: number;
	numForcedGc: number;
	gcCpuFraction: number;
}

export interface GCInfo {
	lastGc: string;
	numGc: number;
	pauseTotalNs: number;
	recentPausesNs: number[];
}

export interface WebSocketConnectionInfo {
	id: string;
	kind: string;
	envId?: string;
	resourceId?: string;
	clientIp?: string;
	userId?: string;
	userAgent?: string;
	startedAt: string;
}

export interface WebSocketMetricsSnapshot {
	projectLogsActive: number;
	containerLogsActive: number;
	containerStats: number;
	containerExec: number;
	systemStats: number;
	serviceLogsActive: number;
	diagnosticsActive: number;
}

export interface WebSocketDiagnostics {
	snapshot: WebSocketMetricsSnapshot;
	connections: WebSocketConnectionInfo[];
}

export interface Diagnostics {
	timestamp: string;
	runtime: RuntimeInfo;
	memory: MemoryInfo;
	gc: GCInfo;
	websocket: WebSocketDiagnostics;
}

export interface ActorHostInfo {
	hostId: string;
	address: string;
	lastHealthCheck: string;
}

export interface ActorInstance {
	actorId: string;
	name?: string;
	hostId: string;
	activatedAt: string;
}

export interface ActorJobFailure {
	jobId: string;
	actorId: string;
	name?: string;
	method: string;
	attempts: number;
	lastError?: string;
	failedAt: string;
}

export interface ActorTypeInfo {
	actorType: string;
	concurrencyLimit: number;
	activeActors: number;
	storedStates: number;
	pendingAlarms: number;
	pendingJobs: number;
	leasedAlarms: number;
	completedJobs: number;
	deadJobs: number;
	nextAlarmAt?: string;
	instances: ActorInstance[];
	recentFailures: ActorJobFailure[];
}

export interface ActorDiagnostics {
	environmentId: string;
	environmentName: string;
	error?: string;
	ready: boolean;
	hosts: ActorHostInfo[];
	actorTypes: ActorTypeInfo[];
	runs: Record<string, number>;
	checkedAt: string;
}

export interface DiagnosticsCommand {
	id: string;
	type: 'refresh' | 'dump' | 'leakScan' | 'profile';
	name?: string;
	seconds?: number;
}

export interface DiagnosticsMessage {
	type: 'snapshot' | 'result';
	id?: string;
	snapshot?: Diagnostics;
	leakReport?: GoroutineLeakReport;
	text?: string;
	data?: string;
	error?: string;
}

export interface LogEntry {
	time: string;
	level: string;
	message: string;
	attrs?: Record<string, unknown>;
}

export interface GoroutineLeakReport {
	count: number;
	profile: string;
	scannedAt: string;
}

export interface GoroutineFrame {
	function: string;
	file: string;
	line: number;
}

export interface GoroutineGroup {
	count: number;
	label: string;
	frames: GoroutineFrame[];
}

export interface GoroutineProfile {
	total: number;
	groups: GoroutineGroup[];
	capturedAt: string;
}

export type PprofProfile =
	| 'heap'
	| 'goroutine'
	| 'goroutineleak'
	| 'allocs'
	| 'block'
	| 'mutex'
	| 'threadcreate'
	| 'profile'
	| 'trace';

export type UpgradeJobResultStatus =
	| 'pending'
	| 'updating'
	| 'updated'
	| 'up_to_date'
	| 'triggered'
	| 'skipped_offline'
	| 'failed';

export type UpgradeJobStage = 'checking' | 'starting' | 'pulling' | 'upgrading' | 'reconnecting' | 'verifying';

export interface UpgradeJobResult {
	environmentId: string;
	environmentName: string;
	status: UpgradeJobResultStatus;
	fromVersion?: string;
	fromDigest?: string;
	toVersion?: string;
	stage?: UpgradeJobStage;
	stageStartedAt?: string;
	percent: number;
	logs?: string[];
	error?: string;
}

export type UpgradeJobStatus = 'pending_restart' | 'running' | 'completed' | 'failed';

export interface UpgradeJob {
	id: string;
	createdAt: string;
	updatedAt?: string;
	status: UpgradeJobStatus;
	userId: string;
	username: string;
	managerVersionAtStart: string;
	managerDigestAtStart: string;
	managerTargetVersion: string;
	results?: UpgradeJobResult[];
	error?: string;
	completedAt?: string;
}

export interface SystemDiskUsage {
	unusedImagesBytes: number;
	buildCacheBytes: number;
	unusedVolumesBytes: number;
	stoppedContainersBytes: number;
}

export interface GPUStats {
	name: string;
	index: number;
	memoryUsed: number;
	memoryTotal: number;
}

export interface SystemStats {
	architecture: string;
	platform: string;
	hostname?: string;
	gpus?: GPUStats[];
	cpuUsage: number;
	memoryUsage: number;
	memoryTotal: number;
	diskUsage?: number;
	diskTotal?: number;
	cpuCount: number;
	gpuCount: number;
}

export type PruneContainerMode = 'none' | 'stopped' | 'olderThan';
export type PruneImageMode = 'none' | 'dangling' | 'all' | 'olderThan';
export type PruneVolumeMode = 'none' | 'anonymous' | 'all';
export type PruneNetworkMode = 'none' | 'unused' | 'olderThan';
export type PruneBuildCacheMode = 'none' | 'unused' | 'all' | 'olderThan';

export interface PruneContainersOptions {
	mode: Exclude<PruneContainerMode, 'none'>;
	until?: string;
}

export interface PruneImagesOptions {
	mode: Exclude<PruneImageMode, 'none'>;
	until?: string;
}

export interface PruneVolumesOptions {
	mode: Exclude<PruneVolumeMode, 'none'>;
}

export interface PruneNetworksOptions {
	mode: Exclude<PruneNetworkMode, 'none'>;
	until?: string;
}

export interface PruneBuildCacheOptions {
	mode: Exclude<PruneBuildCacheMode, 'none'>;
	until?: string;
}

export interface PruneAllRequest {
	containers?: PruneContainersOptions;
	images?: PruneImagesOptions;
	volumes?: PruneVolumesOptions;
	networks?: PruneNetworksOptions;
	buildCache?: PruneBuildCacheOptions;
}

export interface UpgradeCheckResult {
	message: string;
	canUpgrade: boolean;
	error: boolean;
}
