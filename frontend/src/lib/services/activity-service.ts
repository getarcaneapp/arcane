import BaseAPIService, { handleUnauthorizedResponseInternal } from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { Activity, ActivityClearHistoryResult, ActivityDetail } from '$lib/types/activity.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
import { transformPaginationParams } from '$lib/utils/tables';
import { streamCacheBuster } from '$lib/utils/streaming';

class ActivityService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getActivities(options?: SearchPaginationSortRequest, environmentId?: string): Promise<Paginated<Activity>> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/activities`, {
			params,
			suppressAccessDeniedToast: true
		});
		return res.data;
	}

	async getActivity(activityId: string, environmentId?: string, limit = 500): Promise<ActivityDetail> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/activities/${activityId}`, { params: { limit } }));
	}

	async cancelActivity(activityId: string, environmentId?: string): Promise<Activity> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/activities/${encodeURIComponent(activityId)}/cancel`));
	}

	async clearHistory(environmentId?: string): Promise<ActivityClearHistoryResult> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.delete(`/environments/${envId}/activities/history`));
	}

	getActivityStreamUrl(limit = 50): string {
		const baseUrl = this.api.defaults.baseURL.replace(/\/+$/, '');
		const params = new URLSearchParams({ limit: String(limit), _: streamCacheBuster() });
		return `${baseUrl}/activities/stream?${params.toString()}`;
	}

	async openActivityStream(signal: AbortSignal, limit = 50, retry = false): Promise<Response> {
		const response = await fetch(this.getActivityStreamUrl(limit), {
			credentials: 'include',
			headers: { Accept: 'application/x-json-stream' },
			signal
		});
		if (response.status === 401) {
			const action = await handleUnauthorizedResponseInternal('/activities/stream', retry);
			if (action === 'retry') {
				return this.openActivityStream(signal, limit, true);
			}
			if (action === 'redirect') {
				return new Promise<Response>(() => {});
			}
		}
		if (!response.ok) {
			throw new Error(`Activity stream failed with status ${response.status}`);
		}
		return response;
	}
}

export const activityService = new ActivityService();
