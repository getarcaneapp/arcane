import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import type { Event, EventExportFormat, EventSeverityCounts } from '#lib/types/shared.js';
import { filenameFromContentDisposition } from '#lib/utils/browser-download.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

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

	async exportEvents(
		format: EventExportFormat,
		options?: Pick<SearchPaginationSortRequest, 'search' | 'filters'>
	): Promise<{ blob: Blob; filename: string }> {
		const params = { ...transformPaginationParams(options), format };
		const res = await this.api.get('/events/export', { params, responseType: 'blob' });
		return {
			blob: res.data,
			filename: filenameFromContentDisposition(res.headers.get('content-disposition'), `events.${format}`)
		};
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/events/${id}`));
	}
}

export const eventService = new EventService();
