import BaseAPIService from './api-service';
import type { NotificationSettings, TestNotificationResponse, AppriseSettings } from '$lib/types/notification.type';
import { environmentStore } from '$lib/stores/environment.store.svelte';

export default class NotificationService extends BaseAPIService {
	async getSettings(environmentId?: string): Promise<NotificationSettings[]> {
		const envId = environmentId || (await environmentStore.getCurrentEnvironmentId());
		const res = await this.api.get(`/environments/${envId}/notifications/settings`);
		return res.data.map((s: any) => ({
			...s,
			id: s.id.toString()
		}));
	}

	async updateSettings(provider: string, settings: NotificationSettings): Promise<NotificationSettings> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		// Convert id to number for backend
		const payload = {
			...settings,
			id: settings.id ? parseInt(settings.id, 10) : 0
		};
		const res = await this.api.post(`/environments/${envId}/notifications/settings`, payload);
		return {
			...res.data,
			id: res.data.id.toString()
		};
	}

	async deleteSettings(provider: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.api.delete(`/environments/${envId}/notifications/settings/${provider}`);
	}

	async testNotification(provider: string, type: string = 'simple'): Promise<TestNotificationResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/notifications/test/${provider}?type=${type}`));
	}

	async getAppriseSettings(environmentId?: string): Promise<AppriseSettings> {
		const envId = environmentId || (await environmentStore.getCurrentEnvironmentId());
		const res = await this.api.get(`/environments/${envId}/notifications/apprise`);
		return res.data;
	}

	async updateAppriseSettings(settings: AppriseSettings): Promise<AppriseSettings> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.post(`/environments/${envId}/notifications/apprise`, settings);
		return res.data;
	}

	async testAppriseNotification(): Promise<TestNotificationResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/notifications/apprise/test`));
	}
}

export const notificationService = new NotificationService();
