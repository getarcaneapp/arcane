import type { NotificationSettings, NotificationSettingsUpsert, TestNotificationResponse } from '#lib/types/notifications.js';

import BaseAPIService from './api-service';

class NotificationService extends BaseAPIService {
	async getSettings(environmentId: string): Promise<NotificationSettings[]> {
		const res = await this.api.get(`/environments/${environmentId}/notifications/settings`);
		return res.data;
	}

	async updateSettings(
		provider: string,
		settings: NotificationSettingsUpsert,
		environmentId: string
	): Promise<NotificationSettings> {
		// The current backend settings endpoint applies the full notification config, not a provider-specific subresource.
		void provider;
		const res = await this.api.post(`/environments/${environmentId}/notifications/settings`, settings);
		return res.data;
	}

	async testNotification(provider: string, type: string, environmentId: string): Promise<TestNotificationResponse> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/notifications/settings/${provider}`, undefined, {
				params: { action: 'test', type }
			})
		);
	}
}

export const notificationService = new NotificationService();
