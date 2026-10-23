import type { Webhook, WebhookCreated, WebhookCreate, WebhookUpdate } from '#lib/types/environment.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class WebhookAPIService extends BaseAPIService {
	async getWebhooks(environmentId: string, config?: APIRequestConfig): Promise<Webhook[]> {
		const res = await this.api.get(`/environments/${environmentId}/webhooks`, config);
		return res.data.data;
	}

	async create(webhook: WebhookCreate, environmentId: string): Promise<WebhookCreated> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/webhooks`, webhook)) as Promise<WebhookCreated>;
	}

	async update(webhookId: string, data: WebhookUpdate, environmentId: string): Promise<Webhook> {
		return this.handleResponse(this.api.patch(`/environments/${environmentId}/webhooks/${webhookId}`, data)) as Promise<Webhook>;
	}

	async delete(webhookId: string, environmentId: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/environments/${environmentId}/webhooks/${webhookId}`)) as Promise<void>;
	}
}

export const webhookService = new WebhookAPIService();
