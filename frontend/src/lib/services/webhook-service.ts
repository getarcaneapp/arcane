import type { Paginated } from '#lib/types/base.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { WebhookCreate, WebhookUpdate } from '#lib/types/ui/webhook.js';
import type { Webhook, WebhookCreated } from '#lib/types/webhook.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class WebhookAPIService extends BaseAPIService {
	async getWebhooks(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<Paginated<Webhook>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get<Paginated<Webhook>>(`/agents/${environmentId}/webhooks`, { ...config, params });
		return res.data;
	}

	async create(webhook: WebhookCreate, environmentId: string): Promise<WebhookCreated> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/webhooks`, webhook)) as Promise<WebhookCreated>;
	}

	async update(webhookId: string, data: WebhookUpdate, environmentId: string): Promise<Webhook> {
		return this.handleResponse(this.api.patch(`/agents/${environmentId}/webhooks/${webhookId}`, data)) as Promise<Webhook>;
	}

	async delete(webhookId: string, environmentId: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/agents/${environmentId}/webhooks/${webhookId}`)) as Promise<void>;
	}
}

export const webhookService = new WebhookAPIService();
