import { apiClient } from '../api-service';
import type { AppVersionInformation } from '$lib/types/settings';

export interface UpgradeCheckResponse {
	canUpgrade: boolean;
	error: boolean;
	message: string;
}

export interface UpgradeResponse {
	message: string;
	success: boolean;
	error?: string;
}

export interface HealthCheckResult {
	healthy: boolean;
}

export type UpdateAllEnvironmentStatus =
	| 'pending'
	| 'updated'
	| 'triggered'
	| 'skipped_up_to_date'
	| 'skipped_offline'
	| 'failed';

export interface UpdateAllEnvironmentResult {
	environmentId: string;
	environmentName: string;
	status: UpdateAllEnvironmentStatus;
	fromVersion?: string;
	toVersion?: string;
	error?: string;
}

export type UpdateAllJobStatus = 'pending_restart' | 'running' | 'completed' | 'failed';

export interface UpdateAllJob {
	id: string;
	status: UpdateAllJobStatus;
	results?: UpdateAllEnvironmentResult[];
	error?: string;
	createdAt: string;
	completedAt?: string;
}

type ApiResponse<T> = {
	success: boolean;
	data: T;
	message?: string;
};

/**
 * Check if an environment can perform a self-upgrade.
 * @param environmentId - Environment ID (defaults to the local manager, '0')
 */
async function checkUpgradeAvailable(environmentId: string = '0'): Promise<UpgradeCheckResponse> {
	const res = await apiClient.get<UpgradeCheckResponse>(`/environments/${environmentId}/system/upgrade/check`);
	return res.data;
}

/**
 * Trigger a self-upgrade on an environment.
 * @param environmentId - Environment ID (defaults to the local manager, '0')
 */
async function triggerUpgrade(environmentId: string = '0'): Promise<UpgradeResponse> {
	const res = await apiClient.post<UpgradeResponse>(`/environments/${environmentId}/system/upgrade`);
	return res.data;
}

/**
 * Trigger a fleet-wide update, upgrading the manager first and then every online
 * remote environment that has an update available. No client timeout is set: the
 * manager pulls the upgrader image before responding.
 */
async function triggerUpdateAll(): Promise<UpdateAllJob> {
	const res = await apiClient.post<ApiResponse<UpdateAllJob>>('/environments/0/system/upgrade/all');
	return res.data.data;
}

/**
 * Fetch the latest update-all job for live progress polling.
 */
async function getUpdateAllStatus(): Promise<UpdateAllJob> {
	const res = await apiClient.get<ApiResponse<UpdateAllJob>>('/environments/0/system/upgrade/all/status', {
		timeout: 5000
	});
	return res.data.data;
}

/**
 * Check system health
 * @param environmentId - Optional environment ID for remote environments (defaults to local system)
 * @returns Promise with health check result
 */
async function checkHealth(environmentId: string = '0'): Promise<HealthCheckResult> {
	try {
		const endpoint = environmentId === '0' ? '/health' : `/environments/${environmentId}/system/health`;
		const res = await apiClient.head(endpoint, {
			timeout: 3000
		});
		return { healthy: res.status === 200 };
	} catch {
		return { healthy: false };
	}
}

/**
 * Fetch the running version info (including current digest) for the local system (envId=0)
 * or a remote environment.
 */
async function getVersionInfo(environmentId: string = '0'): Promise<AppVersionInformation> {
	if (environmentId === '0') {
		const res = await apiClient.get<AppVersionInformation>('/app-version', { timeout: 5000 });
		return res.data;
	}

	const res = await apiClient.get<ApiResponse<AppVersionInformation>>(`/environments/${environmentId}/version`, {
		timeout: 5000
	});
	return res.data.data;
}

export default {
	checkUpgradeAvailable,
	triggerUpgrade,
	triggerUpdateAll,
	getUpdateAllStatus,
	checkHealth,
	getVersionInfo
};
