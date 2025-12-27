import BaseAPIService from './api-service';
import type {
	GitOpsSyncCreateDto,
	GitOpsSyncUpdateDto,
	GitOpsSync,
	SyncResult,
	SyncStatus,
	BrowseResponse
} from '$lib/types/gitops.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export default class GitOpsSyncService extends BaseAPIService {
	async getSyncs(options?: SearchPaginationSortRequest): Promise<Paginated<GitOpsSync>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/gitops-syncs', { params });
		return res.data;
	}

	async getSync(id: string): Promise<GitOpsSync> {
		return this.handleResponse(this.api.get(`/gitops-syncs/${id}`));
	}

	async createSync(sync: GitOpsSyncCreateDto): Promise<GitOpsSync> {
		return this.handleResponse(this.api.post(`/gitops-syncs`, sync));
	}

	async updateSync(id: string, sync: GitOpsSyncUpdateDto): Promise<GitOpsSync> {
		return this.handleResponse(this.api.put(`/gitops-syncs/${id}`, sync));
	}

	async deleteSync(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/gitops-syncs/${id}`));
	}

	async performSync(id: string): Promise<SyncResult> {
		return this.handleResponse(this.api.post(`/gitops-syncs/${id}/sync`));
	}

	async getSyncStatus(id: string): Promise<SyncStatus> {
		return this.handleResponse(this.api.get(`/gitops-syncs/${id}/status`));
	}

	async browseFiles(id: string, path?: string): Promise<BrowseResponse> {
		const params = path ? { path } : {};
		return this.handleResponse(this.api.get(`/gitops-syncs/${id}/files`, { params }));
	}
}

export const gitOpsSyncService = new GitOpsSyncService();
