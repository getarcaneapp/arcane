// Mirrors backend/types/system/diagnostics.go (camelCase JSON keys).

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
	instances: ActorInstance[] | null;
	recentFailures: ActorJobFailure[] | null;
}

export interface ActorDiagnostics {
	environmentId: string;
	environmentName: string;
	error?: string;
	ready: boolean;
	hosts: ActorHostInfo[] | null;
	actorTypes: ActorTypeInfo[] | null;
	runs: Record<string, number> | null;
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
	/** Base64-encoded pprof bytes for profile results. */
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
