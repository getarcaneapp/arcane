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
}

export interface GPUStats {
	name: string;
	index: number;
	memoryUsed: number; // in MB
	memoryTotal: number; // in MB
}
