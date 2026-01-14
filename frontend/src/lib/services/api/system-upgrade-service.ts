import axios from 'axios';
import type { AppVersionInformation } from '$lib/types/application-configuration';

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

type ApiResponse<T> = {
	success: boolean;
	data: T;
	message?: string;
};

/**
 * Check if the system can perform a self-upgrade
 * @returns Promise with upgrade availability status
 */
async function checkUpgradeAvailable(): Promise<UpgradeCheckResponse> {
	const res = await axios.get<UpgradeCheckResponse>('/api/environments/0/system/upgrade/check');
	return res.data;
}

/**
 * Trigger a system self-upgrade
 * @returns Promise with upgrade initiation result
 */
async function triggerUpgrade(): Promise<UpgradeResponse> {
	const res = await axios.post<UpgradeResponse>('/api/environments/0/system/upgrade');
	return res.data;
}

/**
 * Check system health
 * @param environmentId - Optional environment ID for remote environments (defaults to local system)
 * @returns Promise with health check result
 */
async function checkHealth(environmentId: string = '0'): Promise<HealthCheckResult> {
	try {
		const endpoint = environmentId === '0' ? '/api/health' : `/api/environments/${environmentId}/system/health`;
		const res = await axios.head(endpoint, {
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
		const res = await axios.get<AppVersionInformation>('/api/app-version', { timeout: 5000 });
		return res.data;
	}

	const res = await axios.get<ApiResponse<AppVersionInformation>>(`/api/environments/${environmentId}/version`, {
		timeout: 5000
	});
	return res.data.data;
}

export default {
	checkUpgradeAvailable,
	triggerUpgrade,
	checkHealth,
	getVersionInfo
};
