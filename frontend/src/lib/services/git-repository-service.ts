import type { Paginated } from '#lib/types/base.js';
import type { GitBranchesResponse, GitOpsBrowseResponse, GitRepository, GitRepositoryTestResponse } from '#lib/types/gitops.js';
import type { GitRepositoryCreate, GitRepositoryUpdate } from '#lib/types/ui/gitops.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class GitRepositoryService extends BaseAPIService {
	async getRepositories(options?: SearchPaginationSortRequest): Promise<Paginated<GitRepository>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/gitops/repositories', { params });
		return res.data;
	}

	async getRepository(id: string): Promise<GitRepository> {
		return this.handleResponse(this.api.get(`/gitops/repositories/${id}`));
	}

	async createRepository(repository: GitRepositoryCreate): Promise<GitRepository> {
		return this.handleResponse(this.api.post(`/gitops/repositories`, repository));
	}

	async updateRepository(id: string, repository: GitRepositoryUpdate): Promise<GitRepository> {
		return this.handleResponse(this.api.patch(`/gitops/repositories/${id}`, repository));
	}

	async deleteRepository(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/gitops/repositories/${id}`));
	}

	async testRepository(id: string, branch?: string): Promise<GitRepositoryTestResponse> {
		const params = branch ? { action: 'test', branch } : { action: 'test' };
		return this.handleResponse(this.api.post(`/gitops/repositories/${id}`, {}, { params }));
	}

	async getBranches(id: string): Promise<GitBranchesResponse> {
		return this.handleResponse(this.api.get(`/gitops/repositories/${id}/branches`));
	}

	async browseFiles(id: string, branch: string, path?: string): Promise<GitOpsBrowseResponse> {
		const params = { branch, ...(path && { path }) };
		return this.handleResponse(this.api.get(`/gitops/repositories/${id}/files`, { params }));
	}
}

export const gitRepositoryService = new GitRepositoryService();
