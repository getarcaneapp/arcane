import type { Paginated } from '#lib/types/base.js';
import type {
	DeploymentSnippet,
	EnrollmentToken,
	EnrollmentTokenCreate,
	EnrollmentTokenCreated,
	EnrollmentTokenSecret,
	Environment
} from '#lib/types/environment.js';
import type { EnvironmentCreate, EnvironmentUpdate } from '#lib/types/ui/environment.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { VersionInfo } from '#lib/types/version.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class EnvironmentManagementService extends BaseAPIService {
	async create(dto: EnvironmentCreate): Promise<Environment> {
		const res = await this.api.post<{ data: Environment }>('/agents', dto);
		return res.data.data;
	}

	async getEnvironments(options: SearchPaginationSortRequest): Promise<Paginated<Environment>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get<Paginated<Environment>>('/agents', { params });
		return res.data;
	}

	async get(environmentId: string): Promise<Environment> {
		const res = await this.api.get<{ data: Environment }>(`/agents/${environmentId}`);
		return res.data.data;
	}

	async update(environmentId: string, dto: EnvironmentUpdate): Promise<Environment> {
		const res = await this.api.patch<{ data: Environment }>(`/agents/${environmentId}`, dto);
		return res.data.data;
	}

	async delete(environmentId: string): Promise<void> {
		await this.api.delete(`/agents/${environmentId}`);
	}

	async testConnection(environmentId: string): Promise<{ status: 'online' | 'offline'; message?: string }> {
		const res = await this.api.post<{ data: { status: 'online' | 'offline'; message?: string } }>(
			`/agents/${environmentId}/test`
		);
		return res.data.data;
	}

	async restartTunnel(environmentId: string): Promise<{ reconnected: boolean; sessionId?: string }> {
		const res = await this.api.post<{ data: { reconnected: boolean; sessionId?: string } }>(
			`/agents/${environmentId}/tunnel/restart`
		);
		return res.data.data;
	}

	async sync(environmentId: string): Promise<void> {
		await this.api.post(`/agents/${environmentId}/sync`);
	}

	async generateDeploymentSnippets(environmentId: string): Promise<DeploymentSnippet> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/deployment`));
	}

	async getVersion(environmentId: string, config?: APIRequestConfig): Promise<VersionInfo> {
		const res = await this.api.get<{ data: VersionInfo }>(`/agents/${environmentId}/version`, config);
		return res.data.data;
	}

	async listEnrollmentTokens(): Promise<EnrollmentToken[]> {
		return this.handleResponse(this.api.get('/enrollment-tokens'));
	}

	async createEnrollmentToken(request: EnrollmentTokenCreate): Promise<EnrollmentTokenCreated> {
		return this.handleResponse(this.api.post('/enrollment-tokens', request));
	}

	async getEnrollmentTokenSecret(id: string): Promise<EnrollmentTokenSecret> {
		return this.handleResponse(this.api.get(`/enrollment-tokens/${id}/token`));
	}

	async deleteEnrollmentToken(id: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/enrollment-tokens/${id}`));
	}
}

export const environmentManagementService = new EnvironmentManagementService();
