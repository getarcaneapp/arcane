export interface SystemStats {
	cpuUsage: number;
	memoryUsage: number;
	memoryTotal: number;
	diskUsage?: number;
	diskTotal?: number;
	cpuCount: number;
	architecture: string;
	platform: string;
	hostname?: string;
	gpuCount: number;
	gpus?: GPUStats[];
	goroutines: GoroutineStats;
	threads: number;
	runtime: RuntimeStats;
	runtimeMetrics?: RuntimeMetric[];
}

export interface GoroutineStats {
	idle: number;
	runnable: number;
	running: number;
	syscall: number;
	waiting: number;
	total: number;
	created: number;
}

export interface RuntimeStats {
	heapAlloc: number;
	heapSys: number;
	heapIdle: number;
	heapInuse: number;
	stackInuse: number;
	stackSys: number;
	mSpanInuse: number;
	mSpanSys: number;
	mCacheInuse: number;
	mCacheSys: number;
	gcSys: number;
	nextGC: number;
	lastGC: number;
	numGC: number;
	numForcedGC: number;
	gcCPUFraction: number;
}

export interface RuntimeMetric {
	name: string;
	description: string;
	unit: string;
	kind: string;
	value: string;
}

export interface GPUStats {
	name: string;
	index: number;
	memoryUsed: number; // in MB
	memoryTotal: number; // in MB
}
