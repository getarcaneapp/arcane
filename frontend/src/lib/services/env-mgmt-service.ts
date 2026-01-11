import BaseAPIService from './api-service';
import type { Environment } from '$lib/types/environment.type';
import type { CreateEnvironmentDTO, UpdateEnvironmentDTO } from '$lib/types/environment.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import type { AppVersionInformation } from '$lib/types/application-configuration';
import { transformPaginationParams } from '$lib/utils/params.util';

export default class EnvironmentManagementService extends BaseAPIService {
	async create(dto: CreateEnvironmentDTO): Promise<Environment> {
		const res = await this.api.post('/environments', dto);
		return res.data.data as Environment;
	}

	async getEnvironments(options: SearchPaginationSortRequest): Promise<Paginated<Environment>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/environments', { params });
		return res.data;
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

	async sync(environmentId: string): Promise<void> {
		await this.api.post(`/environments/${environmentId}/sync`);
	}

	async getDeploymentSnippets(environmentId: string): Promise<{ dockerRun: string; dockerCompose: string }> {
		const res = await this.api.get(`/environments/${environmentId}/deployment`);
		return res.data.data as { dockerRun: string; dockerCompose: string };
	}

	async getVersion(environmentId: string): Promise<AppVersionInformation> {
		const res = await this.api.get(`/environments/${environmentId}/version`);
		return res.data.data as AppVersionInformation;
	}
}

export const environmentManagementService = new EnvironmentManagementService();
