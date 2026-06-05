import BaseAPIService from './api-service';
import type { DashboardSnapshot } from '$lib/types/shared';

interface GetDashboardOptions {
	debugAllGood?: boolean;
}

class DashboardService extends BaseAPIService {
	async getDashboardForEnvironment(environmentId: string, options?: GetDashboardOptions): Promise<DashboardSnapshot> {
		const params = options?.debugAllGood ? { debugAllGood: 'true' } : undefined;
		return this.handleResponse(this.api.get(`/environments/${environmentId}/dashboard`, { params }));
	}
}

export const dashboardService = new DashboardService();
