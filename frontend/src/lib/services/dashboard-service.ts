import type { DashboardSectionId, DashboardSummary } from '#lib/types/dashboard.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class DashboardService extends BaseAPIService {
	async getSummary(scope: string, sections: DashboardSectionId[], config?: APIRequestConfig): Promise<DashboardSummary> {
		return this.handleResponse(
			this.api.get(`/agents/${scope}/dashboard`, { ...config, params: { sections: sections.join(',') } })
		);
	}
}

export const dashboardService = new DashboardService();
