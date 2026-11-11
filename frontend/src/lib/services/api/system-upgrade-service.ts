import type { UpgradeCheckResult, UpgradeJob } from '#lib/types/system.js';
import type { VersionInfo } from '#lib/types/version.js';

import { apiClient } from '../api-service';

type ApiResponse<T> = {
	success: boolean;
	data: T;
	message?: string;
};

async function checkUpgradeAvailable(environmentId: string = '0'): Promise<UpgradeCheckResult> {
	const res = await apiClient.get<UpgradeCheckResult>(`/agents/${environmentId}/system/upgrade/check`);
	return res.data;
}

async function startUpgradeJob(environmentIds?: string[]): Promise<UpgradeJob> {
	const res = await apiClient.post<ApiResponse<UpgradeJob>>(
		'/agents/0/system/upgrade/jobs',
		environmentIds?.length ? { environmentIds } : {}
	);
	return res.data.data;
}

async function getCurrentUpgradeJob(): Promise<UpgradeJob> {
	const res = await apiClient.get<ApiResponse<UpgradeJob>>('/agents/0/system/upgrade/jobs/current', {
		timeout: 5000
	});
	return res.data.data;
}

async function getVersionInfo(environmentId: string = '0'): Promise<VersionInfo> {
	if (environmentId === '0') {
		const res = await apiClient.get<VersionInfo>('/app/version', { timeout: 5000 });
		return res.data;
	}

	const res = await apiClient.get<ApiResponse<VersionInfo>>(`/agents/${environmentId}/version`, {
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
