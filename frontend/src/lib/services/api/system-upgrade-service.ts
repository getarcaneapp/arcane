import type { AppVersionInformation } from '#lib/types/settings.js';
import type { UpdateAllJob } from '#lib/types/system-upgrade.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import BaseAPIService, { APIError, apiClient } from '../api-service';

const UPDATE_ALL_RECOVERY_POLL_MS = 3000;
const UPDATE_ALL_RECOVERY_MAX_UNAVAILABLE_MS = 5 * 60 * 1000;
let updateAllRecovery: Promise<void> | null = null;
let updateAllRecoveryAttempt = 0;
let updateAllRecoveryJobId: string | undefined;

export interface UpgradeCheckResponse {
	canUpgrade: boolean;
	error: boolean;
	message: string;
}

export interface UpgradeResponse {
	message: string;
	success: boolean;
	error?: string;
	/**
	 * The environment already runs the newest image, so the upgrade pulled it again and
	 * left the container in place. Callers can stop waiting for a restart.
	 */
	upToDate: boolean;
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
	const res = await apiClient.post<ApiResponse<{ message?: string; upToDate?: boolean }>>(
		`/environments/${environmentId}/system/upgrade`
	);
	return {
		success: res.data.success,
		message: res.data.data?.message ?? res.data.message ?? '',
		upToDate: res.data.data?.upToDate ?? false
	};
}

/**
 * Trigger a fleet-wide update, upgrading every online remote environment that has an
 * update available first and then the manager itself (last) when it has an update.
 * No client timeout is set: the manager pulls the upgrader image before responding.
 */
async function triggerUpdateAll(): Promise<UpdateAllJob> {
	BaseAPIService.setUpgradeInProgress(true);
	const result = await tryCatch(apiClient.post<ApiResponse<UpdateAllJob>>('/environments/0/system/upgrade/all'));
	if (result.error !== null) {
		const error = result.error;
		const uncertain = !(error instanceof APIError) || !error.status || error.status >= 500 || [408, 409].includes(error.status);
		if (!updateAllRecovery) {
			BaseAPIService.setUpgradeInProgress(false);
			const active = uncertain ? (await tryCatch(getUpdateAllStatus())).data : null;
			// A concurrent successful start may have established recovery during this lookup.
			if (!updateAllRecovery && active && (active.status === 'running' || active.status === 'pending_restart')) {
				monitorUpdateAllRecovery(active);
			}
		}
		throw error;
	}
	const job = result.data.data.data;
	monitorUpdateAllRecovery(job);
	return job;
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

// Recovery follows the job even after its dialog closes or unmounts.
function monitorUpdateAllRecovery(job: UpdateAllJob): void {
	if (typeof window === 'undefined') return;
	if (updateAllRecovery && job.id === updateAllRecoveryJobId) return;
	const attempt = ++updateAllRecoveryAttempt;
	updateAllRecoveryJobId = job.id;
	BaseAPIService.setUpgradeInProgress(true);
	updateAllRecovery = (async () => {
		let deadline = Date.now() + UPDATE_ALL_RECOVERY_MAX_UNAVAILABLE_MS;
		for (;;) {
			await new Promise((resolve) => setTimeout(resolve, UPDATE_ALL_RECOVERY_POLL_MS));
			if (attempt !== updateAllRecoveryAttempt) return;
			// An authenticated status read also drives refresh-and-reload after restart.
			const result = await tryCatch(getUpdateAllStatus());
			if (attempt !== updateAllRecoveryAttempt) return;
			if (result.error === null) {
				if (result.data.id !== job.id) break;
				if (result.data.status !== 'running' && result.data.status !== 'pending_restart') {
					if (result.data.results?.some((item) => item.environmentId === '0' && item.status === 'updated')) {
						BaseAPIService.confirmUpgradeRestart();
					}
					break;
				}
				deadline = Date.now() + UPDATE_ALL_RECOVERY_MAX_UNAVAILABLE_MS;
			} else if (Date.now() >= deadline) {
				break;
			}
		}
	})().finally(() => {
		if (attempt !== updateAllRecoveryAttempt) return;
		BaseAPIService.setUpgradeInProgress(false);
		updateAllRecovery = null;
		updateAllRecoveryJobId = undefined;
	});
}

/**
 * Check system health
 * @param environmentId - Optional environment ID for remote environments (defaults to local system)
 * @returns Promise with health check result
 */
async function checkHealth(environmentId: string = '0'): Promise<HealthCheckResult> {
	const operationResult = await tryCatch(
		(async () => {
			const endpoint = environmentId === '0' ? '/health' : `/environments/${environmentId}/system/health`;
			const res = await apiClient.head(endpoint, {
				timeout: 3000
			});
			return { healthy: res.status === 200 };
		})()
	);
	if (operationResult.error !== null) {
		return { healthy: false };
	} else {
		return operationResult.data;
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
	monitorUpdateAllRecovery,
	checkHealth,
	getVersionInfo
};
