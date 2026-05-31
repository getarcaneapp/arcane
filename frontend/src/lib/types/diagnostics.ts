// Mirrors backend/types/system/diagnostics.go (camelCase JSON keys).

export interface RuntimeInfo {
	goroutines: number;
	wsWorkerGoroutines: number;
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

export interface LogEntry {
	time: string;
	level: string;
	message: string;
	attrs?: Record<string, unknown>;
}

export type PprofProfile = 'heap' | 'goroutine' | 'allocs' | 'block' | 'mutex' | 'threadcreate' | 'profile' | 'trace';
