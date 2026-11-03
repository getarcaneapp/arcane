import type { Category } from '#lib/types/category.js';
import type { SearchResponse } from '#lib/types/search.js';

import { apiClient } from './api-service';

class CustomizeSearchService {
	private baseUrl = '/customize';

	async search(query: string): Promise<SearchResponse> {
		const response = await apiClient.post<SearchResponse>(`${this.baseUrl}/search`, {
			query
		});
		return response.data;
	}

	async getCategories(): Promise<Category[]> {
		const response = await apiClient.get<Category[]>(`${this.baseUrl}/categories`);
		return response.data;
	}
}

export const customizeSearchService = new CustomizeSearchService();
