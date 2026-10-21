import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { NotificationSettings, NotificationSettingsUpsert, TestNotificationResponse } from '#lib/types/notifications.js';

import BaseAPIService from './api-service';

class NotificationService extends BaseAPIService {
	async getSettings(environmentId?: string): Promise<NotificationSettings[]> {
		const envId = environmentId || (await environmentStore.getCurrentEnvironmentId());
		const res = await this.api.get(`/environments/${envId}/notifications/settings`);
		return res.data;
	}

	async updateSettings(provider: string, settings: NotificationSettingsUpsert): Promise<NotificationSettings> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		// The current backend settings endpoint applies the full notification config, not a provider-specific subresource.
		void provider;
		const res = await this.api.post(`/environments/${envId}/notifications/settings`, settings);
		return res.data;
	}

	async testNotification(provider: string, type: string = 'simple'): Promise<TestNotificationResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.post(`/environments/${envId}/notifications/settings/${provider}`, undefined, { params: { action: 'test', type } })
		);
	}
}

export const notificationService = new NotificationService();
