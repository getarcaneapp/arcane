import BaseAPIService from './api-service';
import type { NotificationSettings, TestNotificationResponse } from '$lib/types/notifications';
import { environmentStore } from '$lib/stores/environment.store.svelte';

class NotificationService extends BaseAPIService {
	async getSettings(environmentId?: string): Promise<NotificationSettings[]> {
		const envId = environmentId || (await environmentStore.getCurrentEnvironmentId());
		const res = await this.api.get(`/environments/${envId}/notifications/settings`);
		return res.data;
	}

	async updateSettings(provider: string, settings: NotificationSettings): Promise<NotificationSettings> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		// The current backend settings endpoint applies the full notification config, not a provider-specific subresource.
		void provider;
		const res = await this.api.post(`/environments/${envId}/notifications/settings`, settings);
		return res.data;
	}

	async testNotification(provider: string, type: string = 'simple'): Promise<TestNotificationResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const encodedType = encodeURIComponent(type);
		return this.handleResponse(this.api.post(`/environments/${envId}/notifications/test/${provider}?type=${encodedType}`));
	}
}

export const notificationService = new NotificationService();
