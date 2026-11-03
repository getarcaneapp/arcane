import type { Paginated } from '#lib/types/base.js';
import type { Event, EventSeverityCounts } from '#lib/types/event.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class EventService extends BaseAPIService {
	async getEvents(options?: SearchPaginationSortRequest): Promise<Paginated<Event>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/events', { params });
		return res.data;
	}

	async getEnvironmentEvents(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<Paginated<Event>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/events/environment/${environmentId}`, { ...config, params });
		return res.data;
	}

	async getEventStats(): Promise<EventSeverityCounts> {
		const res = await this.api.get('/events/stats');
		return res.data.data;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/events/${id}`));
	}
}

export const eventService = new EventService();
