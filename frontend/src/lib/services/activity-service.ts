import type { Activity, ActivityClearHistoryResult, ActivityDetail } from '#lib/types/activity.type.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class ActivityService extends BaseAPIService {
	async getActivities(
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<Paginated<Activity>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/activities`, {
			...config,
			params,
			suppressAccessDeniedToast: true
		});
		return res.data;
	}

	async getActivity(activityId: string, environmentId: string, limit = 500): Promise<ActivityDetail> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/activities/${activityId}`, { params: { limit } }));
	}

	async cancelActivity(activityId: string, environmentId: string): Promise<Activity> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/activities/${encodeURIComponent(activityId)}`, undefined, {
				params: { action: 'cancel' }
			})
		);
	}

	async clearHistory(environmentId: string): Promise<ActivityClearHistoryResult> {
		return this.handleResponse(this.api.delete(`/environments/${environmentId}/activities/history`));
	}
}

export const activityService = new ActivityService();
