import type { DeploymentSnippets, Environment, EnvironmentCreate, EnvironmentUpdate } from '#lib/types/environment.js';
import type { AppVersionInformation } from '#lib/types/settings.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class EnvironmentManagementService extends BaseAPIService {
	async create(dto: EnvironmentCreate): Promise<Environment> {
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

	async update(environmentId: string, dto: EnvironmentUpdate): Promise<Environment> {
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

	async getDeploymentSnippets(environmentId: string): Promise<DeploymentSnippets> {
		const res = await this.api.get(`/environments/${environmentId}/deployment`);
		return res.data.data as DeploymentSnippets;
	}

	async getVersion(environmentId: string): Promise<AppVersionInformation> {
		const res = await this.api.get(`/environments/${environmentId}/version`);
		return res.data.data as AppVersionInformation;
	}
}

export const environmentManagementService = new EnvironmentManagementService();
