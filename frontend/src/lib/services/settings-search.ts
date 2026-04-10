import { apiClient } from './api-service';
import type { SettingsSearchResponse, SettingsCategory } from '$lib/types/settings-search.type';

export class SettingsSearchService {
	private baseUrl = '/settings';

	async search(query: string): Promise<SettingsSearchResponse> {
		const response = await apiClient.post<SettingsSearchResponse>(`${this.baseUrl}/search`, {
			query
		});
		return response.data;
	}

	async getCategories(): Promise<SettingsCategory[]> {
		const response = await apiClient.get<SettingsCategory[]>(`${this.baseUrl}/categories`);
		return response.data;
	}
}

export const settingsSearchService = new SettingsSearchService();
