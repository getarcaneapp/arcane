import BaseAPIService from './api-service';
import type {
	GitOpsSyncCreateDto,
	GitOpsSyncUpdateDto,
	GitOpsSync,
	SyncResult,
	SyncStatus,
	BrowseResponse,
	ImportGitOpsSyncRequest,
	ImportGitOpsSyncResponse
} from '$lib/types/gitops.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export default class GitOpsSyncService extends BaseAPIService {
	async getSyncs(environmentId: string, options?: SearchPaginationSortRequest): Promise<Paginated<GitOpsSync>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/gitops-syncs`, { params });
		return res.data;
	}

	async getSync(environmentId: string, syncId: string): Promise<GitOpsSync> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/gitops-syncs/${syncId}`));
	}

	async createSync(environmentId: string, sync: GitOpsSyncCreateDto): Promise<GitOpsSync> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/gitops-syncs`, sync));
	}

	async updateSync(environmentId: string, syncId: string, sync: GitOpsSyncUpdateDto): Promise<GitOpsSync> {
		return this.handleResponse(this.api.put(`/environments/${environmentId}/gitops-syncs/${syncId}`, sync));
	}

	async deleteSync(environmentId: string, syncId: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/environments/${environmentId}/gitops-syncs/${syncId}`));
	}

	async performSync(environmentId: string, syncId: string): Promise<SyncResult> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/gitops-syncs/${syncId}/sync`));
	}

	async getSyncStatus(environmentId: string, syncId: string): Promise<SyncStatus> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/gitops-syncs/${syncId}/status`));
	}

	async browseFiles(environmentId: string, syncId: string, path?: string): Promise<BrowseResponse> {
		const params = path ? { path } : {};
		return this.handleResponse(this.api.get(`/environments/${environmentId}/gitops-syncs/${syncId}/files`, { params }));
	}

	async importSyncs(environmentId: string, syncs: ImportGitOpsSyncRequest[]): Promise<ImportGitOpsSyncResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/gitops-syncs/import`, syncs));
	}
}

export const gitOpsSyncService = new GitOpsSyncService();
