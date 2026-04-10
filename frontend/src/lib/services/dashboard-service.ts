import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { DashboardActionItems, DashboardSnapshot } from '$lib/types/dashboard.type';

interface GetDashboardActionItemsOptions {
	debugAllGood?: boolean;
}

export class DashboardService extends BaseAPIService {
	async getDashboard(options?: GetDashboardActionItemsOptions): Promise<DashboardSnapshot> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.getDashboardForEnvironment(envId, options);
	}

	async getDashboardForEnvironment(environmentId: string, options?: GetDashboardActionItemsOptions): Promise<DashboardSnapshot> {
		const params = options?.debugAllGood ? { debugAllGood: 'true' } : undefined;
		return this.handleResponse(this.api.get(`/environments/${environmentId}/dashboard`, { params }));
	}

	async getActionItems(): Promise<DashboardActionItems> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.getActionItemsForEnvironment(envId);
	}

	async getActionItemsForEnvironment(
		environmentId: string,
		options?: GetDashboardActionItemsOptions
	): Promise<DashboardActionItems> {
		const params = options?.debugAllGood ? { debugAllGood: 'true' } : undefined;
		return this.handleResponse(this.api.get(`/environments/${environmentId}/dashboard/action-items`, { params }));
	}
}

export const dashboardService = new DashboardService();
