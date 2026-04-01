import { apiClient } from './api-service';
import type { CustomizeSearchResponse, CustomizeCategory } from '$lib/types/customize-search.type';

export class CustomizeSearchService {
	private baseUrl = '/customize';

	async search(query: string): Promise<CustomizeSearchResponse> {
		const response = await apiClient.post<CustomizeSearchResponse>(`${this.baseUrl}/search`, {
			query
		});
		return response.data;
	}

	async getCategories(): Promise<CustomizeCategory[]> {
		const response = await apiClient.get<CustomizeCategory[]>(`${this.baseUrl}/categories`);
		return response.data;
	}
}

export const customizeSearchService = new CustomizeSearchService();
