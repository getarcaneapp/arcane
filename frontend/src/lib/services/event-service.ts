import BaseAPIService from './api-service';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/shared';
import type { Event, EventSeverityCounts } from '$lib/types/shared';
import { transformPaginationParams } from '$lib/utils/tables';

class EventService extends BaseAPIService {
	async getEvents(options?: SearchPaginationSortRequest): Promise<Paginated<Event>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/events', { params });
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
