import type { Paginated } from '#lib/types/base.js';
import type { DeploymentSnippet, Environment } from '#lib/types/environment.js';
import type { EnvironmentCreate, EnvironmentUpdate } from '#lib/types/ui/environment.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { VersionInfo } from '#lib/types/version.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

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
		const res = await this.api.patch(`/environments/${environmentId}`, dto);
		return res.data.data as Environment;
	}

	async delete(environmentId: string): Promise<void> {
		await this.api.delete(`/environments/${environmentId}`);
	}

	async testConnection(environmentId: string): Promise<{ status: 'online' | 'offline'; message?: string }> {
		const res = await this.api.post(`/environments/${environmentId}/test`);
		return res.data.data as { status: 'online' | 'offline'; message?: string };
	}

	async restartTunnel(environmentId: string): Promise<{ reconnected: boolean; sessionId?: string }> {
		const res = await this.api.post(`/environments/${environmentId}/tunnel/restart`);
		return res.data.data as { reconnected: boolean; sessionId?: string };
	}

	async sync(environmentId: string): Promise<void> {
		await this.api.post(`/environments/${environmentId}/sync`);
	}

	async getDeploymentSnippets(environmentId: string): Promise<DeploymentSnippet> {
		const res = await this.api.get(`/environments/${environmentId}/deployment`);
		return res.data.data as DeploymentSnippet;
	}

	async downloadDeploymentFile(downloadUrl: string): Promise<Blob> {
		const res = await this.api.get<Blob>(downloadUrl, { baseURL: '', responseType: 'blob' });
		return res.data;
	}

	async getVersion(environmentId: string, config?: APIRequestConfig): Promise<VersionInfo> {
		const res = await this.api.get(`/environments/${environmentId}/version`, config);
		return res.data.data as VersionInfo;
	}
}

export const environmentManagementService = new EnvironmentManagementService();
