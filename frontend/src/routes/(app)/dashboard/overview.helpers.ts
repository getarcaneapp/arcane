import { resolve } from '$app/paths';

import { activityTitle } from '#lib/components/activity/activity-labels.js';
import { jobNameLabel } from '#lib/components/job-card/job-status.js';
import * as m from '#lib/paraglide/messages.js';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import type { Activity, ActivityType } from '#lib/types/activity.type.js';
import type { ApiKey } from '#lib/types/auth.js';
import type { ContainerStatusCounts } from '#lib/types/docker.js';
import type { Environment, VulnerabilityRiskFinding } from '#lib/types/environment.js';
import type { JobRunStatus } from '#lib/types/job.js';
import type { AppVersionInformation, JobStatus } from '#lib/types/settings.js';
import type { DashboardEnvironmentOverview, Event, SystemDiskUsage, SystemStats } from '#lib/types/shared.js';
import { isEnvironmentOnline, resolveEnvironmentStatus } from '#lib/utils/docker.js';
import type { EnvironmentRow } from '#lib/utils/environments.js';
import { bytes, formatDateTime, formatRelativeTime, parseInstant } from '#lib/utils/formatting.js';

export function shouldLoadEnvironment(environment: Environment): boolean {
	return environment.enabled && isEnvironmentOnline(environment);
}

export function createBaseEnvironmentOverview(environment: Environment): DashboardEnvironmentOverview {
	return {
		environment,
		containers: { runningContainers: 0, stoppedContainers: 0, totalContainers: 0, unhealthyContainers: 0, updatesAvailable: 0 },
		imageUsageCounts: { imagesInuse: 0, imagesUnused: 0, totalImages: 0, totalImageSize: 0 },
		updates: 0,
		vulnerabilities: 0,
		actionItemCount: 0
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
		case 'pending':
			return m.common_pending();
		case 'error':
			return m.common_error();
		default:
			return m.common_offline();
	}
}

export function getAgentTransportLabel(environment: Environment): string {
	const transport = environment.connected ? environment.agentTransport : undefined;
	switch (transport ?? environment.lastAgentTransport) {
		case 'websocket':
			return m.environments_agent_websocket_label();
		case 'grpc':
			return m.environments_agent_grpc_label();
		default:
			return m.environments_agent_label();
	}
}

export function getAgentConnectionStateLabel(environment: Environment): string {
	switch (resolveEnvironmentStatus(environment)) {
		case 'online':
			return m.environments_agent_status_connected();
		case 'pending':
			return m.environments_agent_status_connecting();
		default:
			return m.disconnected();
	}
}

function getActivityLabelAndValue(environment: Environment): { label: string; raw: string } | null {
	if (environment.lastHeartbeat) return { label: m.environments_agent_last_heartbeat_label(), raw: environment.lastHeartbeat };
	if (environment.connectedAt) return { label: m.environments_agent_connected_since_label(), raw: environment.connectedAt };
	if (environment.lastSeen) return { label: m.dashboard_all_last_seen(), raw: environment.lastSeen };
	return null;
}

export function getActivityMeta(environment: Environment): { label: string; value: string; title: string } {
	if (!isEnvironmentOnline(environment)) {
		const statusLabel = getResolvedStatusLabel(environment);
		return { label: m.activity(), value: statusLabel, title: statusLabel };
	}

	const labelAndValue = getActivityLabelAndValue(environment);

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

export type AttentionSeverity = 'critical' | 'warning' | 'info';
export type AttentionTarget = { kind: 'link'; href: string } | { kind: 'upgrade' };
export type AttentionSort = 'impact' | 'newest';

export interface AttentionItem {
	key: string;
	severity: AttentionSeverity;
	environmentId: string;
	title: string;
	detail: string;
	timestamp: string | null;
	actionLabel: string;
	target: AttentionTarget;
	count?: number;
}

export interface CollapsedActivity {
	key: string;
	activity: Activity;
	ids: string[];
}

export interface EnvironmentJob {
	environmentId: string;
	job: JobStatus;
}

export interface EnvironmentEvent {
	environmentId: string;
	event: Event;
}

export interface AttentionSources {
	environments: Environment[];
	containers: Map<string, ContainerStatusCounts>;
	events: EnvironmentEvent[];
	findings: EnvironmentRow<VulnerabilityRiskFinding>[];
	stats: Map<string, SystemStats | null>;
	jobs: EnvironmentJob[];
	activityGroups: CollapsedActivity[];
	expiringApiKeys: ApiKey[];
	versions: Map<string, AppVersionInformation>;
	upgradable: ReadonlySet<string>;
	now: Temporal.Instant;
}

const ATTENTION_SEVERITY_RANK: Record<AttentionSeverity, number> = { critical: 0, warning: 1, info: 2 };
const CONTAINER_EVENT_WINDOW_HOURS = 1;
const DISK_WARNING_PERCENT = 85;
const DISK_CRITICAL_PERCENT = 95;
const REPEATED_FAILURE_THRESHOLD = 3;
const REPEATED_FAILURE_WINDOW_HOURS = 24;
const FAILED_RUN_STATUSES: ReadonlySet<JobRunStatus> = new Set(['failed', 'partial', 'needs_attention']);

function environmentDetailHref(environmentId: string): string {
	return resolve('/(app)/environments/[id]', { id: environmentId });
}

function failedJobHref(environmentId: string, jobId: string): string {
	const [group = '', ...target] = jobId.split(':');
	if (group === 'gitops-sync') return resolve('/(app)/gitops');
	if (group === 'volume-backup' && target.length > 0) {
		return resolve(`/(app)/volumes/[volumeName]?tab=backups&env=${encodeURIComponent(environmentId)}`, {
			volumeName: target.join(':')
		});
	}
	return resolve(`/(app)/environments/[id]?tab=history&job=${encodeURIComponent(jobId)}`, { id: environmentId });
}

export function collapseActivities(activities: Activity[]): CollapsedActivity[] {
	const groups = new Map<string, CollapsedActivity>();
	for (const activity of activities) {
		const key = [
			activity.type,
			activity.resourceType ?? '',
			activity.resourceId || activity.resourceName || '',
			activity.status,
			activity.sourceEnvironmentId || activity.environmentId
		].join('|');
		const existing = groups.get(key);
		groups.set(key, existing ? { ...existing, ids: [...existing.ids, activity.id] } : { key, activity, ids: [activity.id] });
	}
	return [...groups.values()];
}

function failingActivityHref(activity: Activity, environmentId: string): string {
	switch (activity.type) {
		case 'image_update_check':
		case 'auto_update':
			return resolve('/(app)/updates');
		case 'vulnerability_scan':
		case 'image_patch':
			return resolve('/(app)/security');
		case 'job_run':
			return resolve('/(app)/environments/[id]?tab=history', { id: environmentId });
	}
	if (activity.resourceId && activity.type.startsWith('container_')) {
		return resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(environmentId)}`, {
			containerId: activity.resourceId
		});
	}
	if (activity.resourceId && activity.type.startsWith('project_')) {
		return resolve(`/(app)/projects/[projectId]?env=${encodeURIComponent(environmentId)}`, { projectId: activity.resourceId });
	}
	return environmentDetailHref(environmentId);
}

function failedJobAndRepeatedFailureItems(sources: AttentionSources): AttentionItem[] {
	const cutoff = sources.now.subtract({ hours: REPEATED_FAILURE_WINDOW_HOURS });
	const failedGroups = sources.activityGroups.filter(({ activity }) => activity.status === 'failed');
	const failedJobs = sources.jobs
		.filter(({ job }) => job.lastRun !== undefined && FAILED_RUN_STATUSES.has(job.lastRun.status))
		.map((entry) => {
			const runActivityIds = new Set([
				entry.job.lastRun?.activityId,
				entry.job.lastRun?.outcome.activityId,
				...(entry.job.lastRun?.outcome.targets ?? []).map((target) => target.activityId)
			]);
			return { ...entry, group: failedGroups.find((group) => group.ids.some((id) => runActivityIds.has(id))) };
		});
	const jobGroupKeys = new Set(failedJobs.flatMap(({ group }) => (group ? [group.key] : [])));

	return [
		...failedJobs.map(({ environmentId, job, group }): AttentionItem => ({
			key: `job:${environmentId}:${job.id}`,
			severity: 'warning',
			environmentId,
			title: m.job_failed({ job: group ? activityTitle(group.activity) : jobNameLabel(job) }),
			detail: job.lastError || job.lastRun?.outcome.message || '',
			timestamp: job.lastRun?.finishedAt ?? job.lastRun?.updatedAt ?? null,
			actionLabel: m.common_view(),
			target: { kind: 'link', href: failedJobHref(environmentId, job.id) },
			count: group?.ids.length
		})),
		...failedGroups
			.filter((group) => {
				const failedAt = parseInstant(group.activity.endedAt || group.activity.startedAt);
				return (
					!jobGroupKeys.has(group.key) &&
					group.ids.length >= REPEATED_FAILURE_THRESHOLD &&
					failedAt !== null &&
					Temporal.Instant.compare(failedAt, cutoff) >= 0
				);
			})
			.map(({ key, activity, ids }): AttentionItem => {
				const environmentId = activity.sourceEnvironmentId || activity.environmentId;
				const title = activityTitle(activity);
				const timestamp = activity.endedAt || activity.startedAt;
				return {
					key: `failing:${key}`,
					severity: 'warning',
					environmentId,
					title: m.job_failed({ job: activity.resourceName ? `${title} ${activity.resourceName}` : title }),
					detail: activity.error || formatRelativeTime(timestamp),
					timestamp,
					actionLabel: m.common_view(),
					target: { kind: 'link', href: failingActivityHref(activity, environmentId) },
					count: ids.length
				};
			})
	];
}

function containerEventItems(events: EnvironmentEvent[], now: Temporal.Instant): AttentionItem[] {
	const cutoff = now.subtract({ hours: CONTAINER_EVENT_WINDOW_HOURS });
	const latest = new Map<string, EnvironmentEvent>();
	for (const entry of events) {
		const { event } = entry;
		const at = parseInstant(event.timestamp);
		const exitCode = String(event.metadata?.['exitCode'] ?? '0');
		const abnormal = event.type === 'container.oom' || (event.type === 'container.die' && exitCode !== '0');
		if (!at || Temporal.Instant.compare(at, cutoff) < 0 || !abnormal || !event.resourceId) continue;
		const key = `${entry.environmentId}:${event.resourceId}`;
		const existing = latest.get(key);
		if (!existing || (parseInstant(existing.event.timestamp)?.epochMilliseconds ?? 0) < at.epochMilliseconds) {
			latest.set(key, entry);
		}
	}
	return [...latest.entries()].map(([key, { environmentId, event }]) => {
		const name = event.resourceName || String(event.metadata?.['name'] ?? event.resourceId);
		const oom = event.type === 'container.oom';
		return {
			key: `event:${key}`,
			severity: oom ? 'critical' : 'warning',
			environmentId,
			title: oom
				? m.name_ran_out_of_memory({ name })
				: m.name_exited_with_code({ name, code: String(event.metadata?.['exitCode'] ?? '') }),
			detail: formatRelativeTime(event.timestamp),
			timestamp: event.timestamp,
			actionLabel: m.common_logs(),
			target: {
				kind: 'link',
				href: resolve(`/(app)/containers/[containerId]?tab=logs&env=${encodeURIComponent(environmentId)}`, {
					containerId: event.resourceId ?? ''
				})
			}
		};
	});
}

export function buildAttentionItems(sources: AttentionSources): AttentionItem[] {
	const offline = sources.environments.filter((environment) => !isEnvironmentOnline(environment));
	const online = sources.environments.filter((environment) => isEnvironmentOnline(environment));
	const soonestKeyExpiry = sources.expiringApiKeys
		.flatMap((apiKey) => {
			const expiresAt = parseInstant(apiKey.expiresAt);
			return expiresAt ? [expiresAt] : [];
		})
		.sort(Temporal.Instant.compare)[0];
	const apiKeyItems: AttentionItem[] =
		sources.expiringApiKeys.length > 0
			? [
					{
						key: 'apikeys',
						severity: 'warning',
						environmentId: LOCAL_DOCKER_ENVIRONMENT_ID,
						title: m.count_api_keys_expiring_soon({ count: sources.expiringApiKeys.length }),
						detail: soonestKeyExpiry ? m.expires_time({ time: formatRelativeTime(soonestKeyExpiry) }) : '',
						timestamp: null,
						actionLabel: m.manage(),
						target: { kind: 'link', href: resolve('/(app)/system/api-keys') }
					}
				]
			: [];

	return [
		...offline.map((environment): AttentionItem => ({
			key: `offline:${environment.id}`,
			severity: 'critical',
			environmentId: environment.id,
			title: m.name_is_offline({ name: environment.name }),
			detail: environment.lastSeen
				? m.last_seen_time({ time: formatRelativeTime(environment.lastSeen) })
				: getResolvedStatusLabel(environment),
			timestamp: environment.lastSeen ?? null,
			actionLabel: m.common_view(),
			target: { kind: 'link', href: environmentDetailHref(environment.id) }
		})),
		...online.flatMap((environment): AttentionItem[] => {
			const unhealthy = sources.containers.get(environment.id)?.unhealthyContainers ?? 0;
			return unhealthy > 0
				? [
						{
							key: `unhealthy:${environment.id}`,
							severity: 'critical',
							environmentId: environment.id,
							title: m.count_unhealthy_containers({ count: unhealthy }),
							detail: m.health_status_unhealthy(),
							timestamp: null,
							actionLabel: m.common_view(),
							target: { kind: 'link', href: resolve('/(app)/containers') }
						}
					]
				: [];
		}),
		...containerEventItems(sources.events, sources.now),
		...sources.findings
			.filter((finding) => finding.knownExploited && finding.runningImagesAffected > 0)
			.map((finding): AttentionItem => ({
				key: `kev:${finding.environmentId}:${finding.vulnerabilityId}`,
				severity: 'critical',
				environmentId: finding.environmentId,
				title: m.cve_known_exploited_running({ cve: finding.vulnerabilityId }),
				detail: `${finding.pkgName} → ${finding.fixedVersion || m.vuln_no_fix()}`,
				timestamp: null,
				actionLabel: m.common_view(),
				target: { kind: 'link', href: resolve('/(app)/security') }
			})),
		...online.flatMap((environment): AttentionItem[] => {
			const stats = sources.stats.get(environment.id) ?? null;
			const percent = getDiskMetric(stats);
			if (percent === null || percent < DISK_WARNING_PERCENT) return [];
			return [
				{
					key: `disk:${environment.id}`,
					severity: percent >= DISK_CRITICAL_PERCENT ? 'critical' : 'warning',
					environmentId: environment.id,
					title: m.disk_percent_full({ percent: Math.round(percent) }),
					detail: getCapacityLabel(stats?.diskUsage, stats?.diskTotal),
					timestamp: null,
					actionLabel: m.common_view(),
					target: { kind: 'link', href: environmentDetailHref(environment.id) }
				}
			];
		}),
		...failedJobAndRepeatedFailureItems(sources),
		...apiKeyItems,
		...online.flatMap((environment): AttentionItem[] => {
			const version = sources.versions.get(environment.id);
			if (!version?.updateAvailable) return [];
			const upgradable = sources.upgradable.has(environment.id);
			return [
				{
					key: `version:${environment.id}`,
					severity: 'info',
					environmentId: environment.id,
					title: m.sidebar_update_available_tooltip({
						version: version.newestVersion || version.newestDigest?.slice(0, 12) || ''
					}),
					detail: version.displayVersion || version.currentTag || version.currentVersion,
					timestamp: version.releasedAt ?? null,
					actionLabel: upgradable ? m.upgrade() : m.common_view(),
					target: upgradable ? { kind: 'upgrade' } : { kind: 'link', href: environmentDetailHref(environment.id) }
				}
			];
		})
	];
}

export function sortAttentionItems(items: AttentionItem[], sort: AttentionSort): AttentionItem[] {
	const time = (item: AttentionItem) => parseInstant(item.timestamp)?.epochMilliseconds ?? 0;
	const rank = (item: AttentionItem) => ATTENTION_SEVERITY_RANK[item.severity];
	return [...items].sort((a, b) =>
		sort === 'impact' ? rank(a) - rank(b) || time(b) - time(a) : time(b) - time(a) || rank(a) - rank(b)
	);
}

const DEPLOY_ACTIVITY_TYPES: ReadonlySet<ActivityType> = new Set(['project_deploy', 'project_redeploy', 'container_redeploy']);

export function isDeployActivity(activity: Activity): boolean {
	return DEPLOY_ACTIVITY_TYPES.has(activity.type);
}

export function totalReclaimableBytes(usage: SystemDiskUsage): number {
	return usage.unusedImagesBytes + usage.buildCacheBytes + usage.unusedVolumesBytes + usage.stoppedContainersBytes;
}
