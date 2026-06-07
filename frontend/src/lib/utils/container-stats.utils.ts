import type { ContainerStats } from '$lib/types/container.type';

export function calculateCPUPercent(stats: ContainerStats | null): number {
	if (!stats?.cpu_stats || !stats?.precpu_stats) return 0;

	const cpuDelta = stats.cpu_stats.cpu_usage.total_usage - (stats.precpu_stats.cpu_usage?.total_usage || 0);
	const systemDelta = stats.cpu_stats.system_cpu_usage - (stats.precpu_stats.system_cpu_usage || 0);

	if (systemDelta > 0 && cpuDelta > 0) {
		const cpuPercent = (cpuDelta / systemDelta) * 100.0;
		return Math.min(Math.max(cpuPercent, 0), 100);
	}
	return 0;
}

export function calculateMemoryPercent(stats: ContainerStats | null): number {
	if (!stats?.memory_stats) return 0;

	const usage = calculateMemoryUsage(stats);
	const limit = stats.memory_stats.limit || 0;

	if (limit > 0) {
		const percent = (usage / limit) * 100;
		return Math.min(Math.max(percent, 0), 100);
	}
	return 0;
}

export function calculateMemoryUsage(stats: ContainerStats | null): number {
	if (!stats?.memory_stats) return 0;

	const usage = stats.memory_stats.usage || 0;
	const inactiveFile = stats.memory_stats.stats?.inactive_file || 0;
	return Math.max(usage - inactiveFile, 0);
}
