import BaseAPIService from './api-service';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
import type { Event } from '$lib/types/event.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export default class EventService extends BaseAPIService {
	async getEvents(options?: SearchPaginationSortRequest): Promise<Paginated<Event>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/events', { params });
		return res.data;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/events/${id}`));
	}
}

export const eventService = new EventService();
