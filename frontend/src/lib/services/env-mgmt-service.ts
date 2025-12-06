import BaseAPIService from './api-service';
import type { Environment } from '$lib/types/environment.type';
import type {
	CreateEnvironmentDTO,
	UpdateEnvironmentDTO,
	EnvironmentFilter,
	CreateEnvironmentFilterDTO,
	UpdateEnvironmentFilterDTO
} from '$lib/types/environment.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export interface EnvironmentFilterOptions extends SearchPaginationSortRequest {
	tags?: string[];
	excludeTags?: string[];
	tagMode?: 'any' | 'all';
	status?: 'online' | 'offline';
}

export default class EnvironmentManagementService extends BaseAPIService {
	async create(dto: CreateEnvironmentDTO): Promise<Environment> {
		const res = await this.api.post('/environments', dto);
		return res.data.data as Environment;
	}

	async getEnvironments(options: EnvironmentFilterOptions = {}): Promise<Paginated<Environment>> {
		const params = transformPaginationParams(options);

		// Add tag-specific filters
		if (options.tags?.length) {
			params.tags = options.tags.join(',');
		}
		if (options.excludeTags?.length) {
			params.excludeTags = options.excludeTags.join(',');
		}
		if (options.tagMode) {
			params.tagMode = options.tagMode;
		}
		if (options.status) {
			params.status = options.status;
		}

		const res = await this.api.get('/environments', { params });
		return res.data;
	}

	async getAllTags(): Promise<string[]> {
		const res = await this.api.get('/environments/tags');
		return res.data.data as string[];
	}

	async get(environmentId: string): Promise<Environment> {
		const res = await this.api.get(`/environments/${environmentId}`);
		return res.data.data as Environment;
	}

	async update(environmentId: string, dto: UpdateEnvironmentDTO): Promise<Environment> {
		const res = await this.api.put(`/environments/${environmentId}`, dto);
		return res.data.data as Environment;
	}

	async delete(environmentId: string): Promise<void> {
		await this.api.delete(`/environments/${environmentId}`);
	}

	async testConnection(environmentId: string, apiUrl?: string): Promise<{ status: 'online' | 'offline'; message?: string }> {
		const res = await this.api.post(`/environments/${environmentId}/test`, apiUrl ? { apiUrl } : undefined);
		return res.data.data as { status: 'online' | 'offline'; message?: string };
	}

	async syncRegistries(environmentId: string): Promise<void> {
		await this.api.post(`/environments/${environmentId}/sync-registries`);
	}

	async getSavedFilters(): Promise<EnvironmentFilter[]> {
		const res = await this.api.get('/environments/filters');
		return res.data.data as EnvironmentFilter[];
	}

	async getSavedFilter(filterId: string): Promise<EnvironmentFilter> {
		const res = await this.api.get(`/environments/filters/${filterId}`);
		return res.data.data as EnvironmentFilter;
	}

	async createSavedFilter(dto: CreateEnvironmentFilterDTO): Promise<EnvironmentFilter> {
		const res = await this.api.post('/environments/filters', dto);
		return res.data.data as EnvironmentFilter;
	}

	async updateSavedFilter(filterId: string, dto: UpdateEnvironmentFilterDTO): Promise<EnvironmentFilter> {
		const res = await this.api.put(`/environments/filters/${filterId}`, dto);
		return res.data.data as EnvironmentFilter;
	}

	async deleteSavedFilter(filterId: string): Promise<void> {
		await this.api.delete(`/environments/filters/${filterId}`);
	}
}

export const environmentManagementService = new EnvironmentManagementService();
