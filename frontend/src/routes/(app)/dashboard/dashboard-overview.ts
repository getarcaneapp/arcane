import * as m from '#lib/paraglide/messages.js';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte';
import type { Environment } from '#lib/types/environment';
import type { DashboardEnvironmentOverview, DashboardOverviewSummary, SystemStats } from '#lib/types/shared';
import { bytes, formatDateTime, formatRelativeTime, parseInstant } from '#lib/utils/formatting';
import { isEnvironmentOnline, resolveEnvironmentStatus } from '#lib/utils/docker';

export function shouldLoadEnvironment(environment: Environment): boolean {
	return environment.enabled && isEnvironmentOnline(environment);
}

export function createBaseEnvironmentOverview(environment: Environment): DashboardEnvironmentOverview {
	return {
		environment,
		containers: { runningContainers: 0, stoppedContainers: 0, totalContainers: 0 },
		imageUsageCounts: { imagesInuse: 0, imagesUnused: 0, totalImages: 0, totalImageSize: 0 },
		actionItems: { items: [] },
		settings: {},
		snapshotState: 'skipped'
	};
}

export function buildOverviewSummary(items: DashboardEnvironmentOverview[]): DashboardOverviewSummary {
	return {
		totalEnvironments: items.length,
		reachableEnvironments: items.filter((item) => item.environment.enabled && isEnvironmentOnline(item.environment)).length,
		unavailableEnvironments: items.filter((item) => item.environment.enabled && !isEnvironmentOnline(item.environment)).length,
		disabledEnvironments: items.filter((item) => !item.environment.enabled).length,
		totalContainers: items.reduce((total, item) => total + item.containers.totalContainers, 0),
		runningContainers: items.reduce((total, item) => total + item.containers.runningContainers, 0),
		stoppedContainers: items.reduce((total, item) => total + item.containers.stoppedContainers, 0),
		totalImages: items.reduce((total, item) => total + item.imageUsageCounts.totalImages, 0),
		imagesInUse: items.reduce((total, item) => total + item.imageUsageCounts.imagesInuse, 0),
		imagesUnused: items.reduce((total, item) => total + item.imageUsageCounts.imagesUnused, 0),
		totalVolumes: items.reduce((total, item) => total + (item.volumeUsageCounts?.total ?? 0), 0),
		volumesInUse: items.reduce((total, item) => total + (item.volumeUsageCounts?.inuse ?? 0), 0),
		volumesUnused: items.reduce((total, item) => total + (item.volumeUsageCounts?.unused ?? 0), 0)
	};
}

export function getRoleBadge(environment: Environment): { text: string; variant: 'primary' | 'gray' } {
	return environment.id === LOCAL_DOCKER_ENVIRONMENT_ID
		? { text: m.manager(), variant: 'primary' }
		: { text: m.agent(), variant: 'gray' };
}

export function getResolvedStatusLabel(environment: Environment): string {
	switch (resolveEnvironmentStatus(environment)) {
		case 'online':
			return m.common_online();
		case 'standby':
			return m.common_standby();
		case 'pending':
			return m.common_pending();
		case 'error':
			return m.common_error();
		default:
			return m.common_offline();
	}
}

export function getActivityMeta(environment: Environment): { label: string; value: string; title: string } {
	if (!isEnvironmentOnline(environment)) {
		const statusLabel = getResolvedStatusLabel(environment);
		return { label: m.activity(), value: statusLabel, title: statusLabel };
	}

	const labelAndValue = environment.lastHeartbeat
		? { label: m.environments_edge_last_heartbeat_label(), raw: environment.lastHeartbeat }
		: environment.lastPollAt
			? { label: m.environments_edge_last_poll_label(), raw: environment.lastPollAt }
			: environment.connectedAt
				? { label: m.environments_edge_connected_since_label(), raw: environment.connectedAt }
				: environment.lastSeen
					? { label: m.dashboard_all_last_seen(), raw: environment.lastSeen }
					: null;

	if (!labelAndValue?.raw) return { label: m.activity(), value: m.common_never(), title: m.common_never() };
	const parsed = parseInstant(labelAndValue.raw);
	if (!parsed) return { label: labelAndValue.label, value: m.common_unknown(), title: m.common_unknown() };
	return { label: labelAndValue.label, value: formatRelativeTime(parsed), title: formatDateTime(parsed) };
}

export function formatPercent(value: number | null | undefined): string {
	return value === null || value === undefined ? '--' : `${value.toFixed(1)}%`;
}

export function getCpuMetric(stats: SystemStats | null): number | null {
	return stats?.cpuUsage ?? null;
}

export function getMemoryMetric(stats: SystemStats | null): number | null {
	return stats?.memoryUsage === undefined || !stats.memoryTotal ? null : (stats.memoryUsage / stats.memoryTotal) * 100;
}

export function getDiskMetric(stats: SystemStats | null): number | null {
	return stats?.diskUsage === undefined || !stats.diskTotal || stats.diskTotal <= 0
		? null
		: (stats.diskUsage / stats.diskTotal) * 100;
}

export function getCpuMetricLabel(stats: SystemStats | null): string {
	return stats ? `${stats.cpuCount ?? 0} ${m.common_cpus()}` : '--';
}

export function getCapacityLabel(used: number | undefined, total: number | undefined): string {
	if (used === undefined || total === undefined || total <= 0) return '--';
	return `${bytes.format(used, { unitSeparator: ' ' }) ?? '-'} / ${bytes.format(total, { unitSeparator: ' ' }) ?? '-'}`;
}

export function getGpuMetric(stats: SystemStats | null): number | null {
	const gpus = stats?.gpus?.filter((gpu) => gpu.memoryTotal > 0) ?? [];
	if (gpus.length === 0) return null;
	return gpus.reduce((sum, gpu) => sum + (gpu.memoryUsed / gpu.memoryTotal) * 100, 0) / gpus.length;
}

export function getGpuMetricLabel(stats: SystemStats | null): string {
	const count = stats?.gpuCount ?? 0;
	return count > 0 ? `${count} ${count === 1 ? m.dashboard_meter_gpu_device() : m.dashboard_meter_gpu_devices()}` : '--';
}

export function formatContainerOverviewLabel(summary: DashboardOverviewSummary): string {
	return summary.totalContainers === 0
		? m.dashboard_all_no_containers()
		: m.dashboard_all_container_summary({ running: summary.runningContainers, stopped: summary.stoppedContainers });
}

export function formatImageOverviewLabel(summary: DashboardOverviewSummary): string {
	return summary.totalImages === 0
		? m.dashboard_all_no_images()
		: m.dashboard_all_image_summary({ inUse: summary.imagesInUse, unused: summary.imagesUnused });
}

export function formatVolumeOverviewLabel(summary: DashboardOverviewSummary): string {
	return summary.totalVolumes === 0
		? m.dashboard_all_no_volumes()
		: m.dashboard_all_volume_summary({ inUse: summary.volumesInUse, unused: summary.volumesUnused });
}
