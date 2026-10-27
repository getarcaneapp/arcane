import type { NotificationSettings, NotificationSettingsUpsert, TestNotificationResponse } from '#lib/types/notifications.js';

import BaseAPIService from './api-service';

class NotificationService extends BaseAPIService {
	async getSettings(): Promise<NotificationSettings[]> {
		const res = await this.api.get('/notifications/settings');
		return res.data;
	}

	async updateSettings(settings: NotificationSettingsUpsert): Promise<NotificationSettings> {
		const res = await this.api.post('/notifications/settings', settings);
		return res.data;
	}

	async testNotification(provider: string, type: string): Promise<TestNotificationResponse> {
		return this.handleResponse(
			this.api.post(`/notifications/settings/${provider}`, undefined, {
				params: { action: 'test', type }
			})
		);
	}
}

export const notificationService = new NotificationService();
