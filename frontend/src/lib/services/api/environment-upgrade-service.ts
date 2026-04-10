import { apiClient } from '../api-service';

export interface EnvironmentUpgradeCheckResponse {
	canUpgrade: boolean;
	error: boolean;
	message: string;
}

export interface EnvironmentUpgradeResponse {
	message: string;
	success: boolean;
	error?: string;
}

/**
 * Check if a remote environment can perform a self-upgrade
 * @param environmentId - The ID of the environment to check
 * @returns Promise with upgrade availability status
 */
async function checkEnvironmentUpgradeAvailable(environmentId: string): Promise<EnvironmentUpgradeCheckResponse> {
	const res = await apiClient.get<EnvironmentUpgradeCheckResponse>(`/environments/${environmentId}/system/upgrade/check`);
	return res.data;
}

/**
 * Trigger a self-upgrade on a remote environment
 * @param environmentId - The ID of the environment to upgrade
 * @returns Promise with upgrade initiation result
 */
async function triggerEnvironmentUpgrade(environmentId: string): Promise<EnvironmentUpgradeResponse> {
	const res = await apiClient.post<EnvironmentUpgradeResponse>(`/environments/${environmentId}/system/upgrade`);
	return res.data;
}

export default {
	checkEnvironmentUpgradeAvailable,
	triggerEnvironmentUpgrade
};
