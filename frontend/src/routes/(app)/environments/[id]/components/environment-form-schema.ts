import { z } from 'zod/v4';

import { m } from '#lib/paraglide/messages.js';

export const environmentUpdateSchema = z.object({
	name: z.string().min(1),
	enabled: z.boolean()
});

export const environmentFormSchema = environmentUpdateSchema.extend({
	pollingEnabled: z.boolean(),
	imageEventWatcherEnabled: z.boolean(),
	autoUpdate: z.boolean(),
	autoInjectEnv: z.boolean(),
	followProjectSymlinks: z.boolean(),
	defaultDeployPullPolicy: z.enum(['missing', 'always', 'never']),
	defaultShell: z.string(),
	projectsDirectory: z.string(),
	templatesDirectory: z.string(),
	swarmStackSourcesDirectory: z.string(),
	diskUsagePath: z.string(),
	maxImageUploadSize: z.coerce.number(),
	gitSyncMaxFiles: z.coerce.number().int().nonnegative(),
	gitSyncMaxTotalSizeMb: z.coerce.number().int().nonnegative(),
	gitSyncMaxBinarySizeMb: z.coerce.number().int().nonnegative(),
	baseServerUrl: z.string(),
	scheduledPruneEnabled: z.boolean(),
	pruneContainerMode: z.enum(['none', 'stopped', 'olderThan']),
	pruneContainerUntil: z.string(),
	pruneImageMode: z.enum(['none', 'dangling', 'all', 'olderThan']),
	pruneImageUntil: z.string(),
	pruneVolumeMode: z.enum(['none', 'anonymous', 'all']),
	pruneNetworkMode: z.enum(['none', 'unused', 'olderThan']),
	pruneNetworkUntil: z.string(),
	pruneBuildCacheMode: z.enum(['none', 'unused', 'all', 'olderThan']),
	pruneBuildCacheUntil: z.string(),
	featureVulnerabilityManagementEnabled: z.boolean(),
	featureSwarmEnabled: z.boolean(),
	vulnerabilityScanEnabled: z.boolean(),
	toolsImageRegistry: z.enum(['ghcr.io', 'docker.io']),
	updateCheckRegistry: z.enum(['auto', 'ghcr.io', 'docker.io', 'public.ecr.aws']),
	trivyDbRegistry: z.enum(['ghcr.io', 'docker.io']),
	trivyNetwork: z.string(),
	trivySecurityOpts: z.string(),
	trivyPrivileged: z.boolean(),
	trivyResourceLimitsEnabled: z.boolean(),
	trivyCpuLimit: z.coerce.number().nonnegative(),
	trivyMemoryLimitMb: z.coerce.number().int().nonnegative(),
	trivyConcurrentScanContainers: z.coerce.number().int().min(1, m.security_trivy_concurrent_scan_containers_min()),
	trivyServerEnabled: z.boolean(),
	trivyServerUrl: z.string(),
	trivyServerToken: z.string(),
	trivyIgnoreUnfixed: z.boolean(),
	vulnerabilityThreatIntelEnabled: z.boolean(),
	trivyConfig: z.string(),
	trivyIgnore: z.string(),
	imagePatchSuffix: z.string(),
	imagePatchTimeoutSec: z.coerce.number().int().min(1),
	imagePatchAllPlatforms: z.boolean(),
	imageAutoPatchEnabled: z.boolean(),
	lifecycleEnabled: z.boolean(),
	lifecycleDefaultRunnerImage: z.string(),
	lifecycleMaxTimeoutSec: z.coerce.number().int().nonnegative(),
	autoUpdateDelay: z.coerce.number().int().min(0).max(8760),
	autoUpdateExcludedContainers: z.string().optional(),
	autoHealEnabled: z.boolean(),
	autoHealExcludedContainers: z.string(),
	autoHealMaxRestarts: z.coerce.number().int().min(1),
	autoHealRestartWindow: z.coerce.number().int().min(1),
	dockerApiTimeout: z.coerce.number().int().nonnegative(),
	dockerImagePullTimeout: z.coerce.number().int().nonnegative(),
	deployWaitTimeout: z.coerce.number().int().nonnegative(),
	trivyScanTimeout: z.coerce.number().int().nonnegative(),
	gitOperationTimeout: z.coerce.number().int().nonnegative(),
	httpClientTimeout: z.coerce.number().int().nonnegative(),
	registryTimeout: z.coerce.number().int().nonnegative(),
	registryTagTimeout: z.coerce.number().int().nonnegative(),
	proxyRequestTimeout: z.coerce.number().int().nonnegative(),
	activityHistoryRetentionDays: z.coerce.number().int().nonnegative(),
	upgradeLogRetentionDays: z.coerce.number().int().min(0).max(3650),
	activityHistoryMaxEntries: z.coerce.number().int().nonnegative(),
	maxConcurrentActivities: z.coerce.number().int().nonnegative(),
	buildProvider: z.enum(['local', 'depot']),
	buildsDirectory: z.string(),
	buildTimeout: z.coerce.number().int().nonnegative(),
	buildHistoryRetentionDays: z.coerce.number().int().nonnegative(),
	depotProjectId: z.string(),
	depotToken: z.string()
});

export function requireTrivyServerUrl(
	data: { trivyServerEnabled?: boolean; trivyServerUrl?: string },
	ctx: z.RefinementCtx
): void {
	if (data.trivyServerEnabled && !data.trivyServerUrl?.trim()) {
		ctx.addIssue({
			code: 'custom',
			message: m.security_trivy_server_url_required(),
			path: ['trivyServerUrl']
		});
	}
}

export type EnvironmentFormValues = z.infer<typeof environmentFormSchema>;
