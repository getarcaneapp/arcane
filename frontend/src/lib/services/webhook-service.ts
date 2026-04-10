import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { Webhook, WebhookCreated, CreateWebhook, UpdateWebhook } from '$lib/types/webhook.type';

export default class WebhookAPIService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getWebhooks(environmentId?: string): Promise<Webhook[]> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const res = await this.api.get(`/environments/${envId}/webhooks`);
		return res.data.data;
	}

	async create(webhook: CreateWebhook, environmentId?: string): Promise<WebhookCreated> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/webhooks`, webhook)) as Promise<WebhookCreated>;
	}

	async update(webhookId: string, data: UpdateWebhook, environmentId?: string): Promise<Webhook> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.patch(`/environments/${envId}/webhooks/${webhookId}`, data)) as Promise<Webhook>;
	}

	async delete(webhookId: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.delete(`/environments/${envId}/webhooks/${webhookId}`)) as Promise<void>;
	}
}

export const webhookService = new WebhookAPIService();
