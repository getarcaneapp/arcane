import type { AppVersionInformation } from '#lib/types/settings.js';
import type { UpgradeJob } from '#lib/types/system-upgrade.js';

import { apiClient } from '../api-service';

export interface UpgradeCheckResponse {
	canUpgrade: boolean;
	error: boolean;
	message: string;
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
async function getVersionInfo(environmentId: string = '0'): Promise<AppVersionInformation> {
	if (environmentId === '0') {
		const res = await apiClient.get<AppVersionInformation>('/app/version', { timeout: 5000 });
		return res.data;
	}

	const res = await apiClient.get<ApiResponse<AppVersionInformation>>(`/environments/${environmentId}/version`, {
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
