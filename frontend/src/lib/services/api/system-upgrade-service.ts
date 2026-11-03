import type { UpgradeCheckResult, UpgradeJob } from '#lib/types/system.js';
import type { VersionInfo } from '#lib/types/version.js';

import { apiClient } from '../api-service';

type ApiResponse<T> = {
	success: boolean;
	data: T;
	message?: string;
};

/**
 * Check if an environment can perform a self-upgrade.
 * @param environmentId - Environment ID (defaults to the local manager, '0')
 */
async function checkUpgradeAvailable(environmentId: string = '0'): Promise<UpgradeCheckResult> {
	const res = await apiClient.get<UpgradeCheckResult>(`/environments/${environmentId}/system/upgrade/check`);
	return res.data;
}

async function startUpgradeJob(environmentIds?: string[]): Promise<UpgradeJob> {
	const res = await apiClient.post<ApiResponse<UpgradeJob>>(
		'/environments/0/system/upgrade/jobs',
		environmentIds?.length ? { environmentIds } : {}
	);
	return res.data.data;
}

async function getCurrentUpgradeJob(): Promise<UpgradeJob> {
	const res = await apiClient.get<ApiResponse<UpgradeJob>>('/environments/0/system/upgrade/jobs/current', {
		timeout: 5000
	});
	return res.data.data;
}

/**
 * Fetch the running version info (including current digest) for the local system (envId=0)
 * or a remote environment.
 */
async function getVersionInfo(environmentId: string = '0'): Promise<VersionInfo> {
	if (environmentId === '0') {
		const res = await apiClient.get<VersionInfo>('/app/version', { timeout: 5000 });
		return res.data;
	}

	const res = await apiClient.get<ApiResponse<VersionInfo>>(`/environments/${environmentId}/version`, {
		timeout: 5000
	});
	return res.data.data;
}

export default {
	checkUpgradeAvailable,
	startUpgradeJob,
	getCurrentUpgradeJob,
	getVersionInfo
};
