import { z } from 'zod/v4';
import { m } from '$lib/paraglide/messages';

export const environmentFormSchema = z
	.object({
		name: z.string().min(1),
		enabled: z.boolean(),
		apiUrl: z.string(),
		pollingEnabled: z.boolean(),
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
		vulnerabilityScanEnabled: z.boolean(),
		trivyImage: z.string(),
		trivyNetwork: z.string(),
		trivySecurityOpts: z.string(),
		trivyPrivileged: z.boolean(),
		trivyPreserveCacheOnVolumePrune: z.boolean(),
		trivyResourceLimitsEnabled: z.boolean(),
		trivyCpuLimit: z.coerce.number().int(m.security_session_timeout_integer()).nonnegative(),
		trivyMemoryLimitMb: z.coerce.number().int().nonnegative(),
		trivyConcurrentScanContainers: z.coerce.number().int().min(1, m.security_trivy_concurrent_scan_containers_min()),
		trivyServerEnabled: z.boolean(),
		trivyServerUrl: z.string(),
		trivyServerToken: z.string(),
		trivyIgnoreUnfixed: z.boolean(),
		autoUpdateExcludedContainers: z.string().optional(),
		autoHealEnabled: z.boolean(),
		autoHealExcludedContainers: z.string(),
		autoHealMaxRestarts: z.coerce.number().int().min(1),
		autoHealRestartWindow: z.coerce.number().int().min(1)
	})
	.superRefine((data, ctx) => {
		if (data.trivyServerEnabled && !data.trivyServerUrl.trim()) {
			ctx.addIssue({
				code: 'custom',
				message: m.security_trivy_server_url_required(),
				path: ['trivyServerUrl']
			});
		}
	});

export type EnvironmentFormValues = z.infer<typeof environmentFormSchema>;
