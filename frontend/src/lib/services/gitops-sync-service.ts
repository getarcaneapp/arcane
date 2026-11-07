import type { PaginatedWithCounts } from '#lib/types/base.js';
import type {
	GitOpsBackupHistoryResponse,
	GitOpsBackupPreview,
	GitOpsBackupRevision,
	GitOpsBrowseResponse,
	GitOpsResolveBackupConflictRequest,
	GitOpsSync,
	GitOpsSyncCounts,
	GitOpsSyncResult,
	GitOpsSyncStatus,
	ImportGitOpsSyncRequest,
	ImportGitOpsSyncResponse
} from '#lib/types/gitops.js';
import type { GitOpsSyncCreate, GitOpsSyncUpdate } from '#lib/types/ui/gitops.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class GitOpsSyncService extends BaseAPIService {
	async getSyncs(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<PaginatedWithCounts<GitOpsSync, GitOpsSyncCounts>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/gitops/syncs`, { ...config, params });
		return res.data;
	}

	async getSync(environmentId: string, syncId: string): Promise<GitOpsSync> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/gitops/syncs/${syncId}`));
	}

	async createSync(environmentId: string, sync: GitOpsSyncCreate): Promise<GitOpsSync> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/gitops/syncs`, sync));
	}

	async updateSync(environmentId: string, syncId: string, sync: GitOpsSyncUpdate): Promise<GitOpsSync> {
		return this.handleResponse(this.api.patch(`/environments/${environmentId}/gitops/syncs/${syncId}`, sync));
	}

	async deleteSync(environmentId: string, syncId: string, removeStack = false): Promise<void> {
		return this.handleResponse(
			this.api.delete(`/environments/${environmentId}/gitops/syncs/${syncId}`, { params: removeStack ? { removeStack } : undefined })
		);
	}

	async performSync(environmentId: string, syncId: string): Promise<GitOpsSyncResult> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/gitops/syncs/${syncId}`, undefined, { params: { action: 'sync' } })
		);
	}

	async getSyncStatus(environmentId: string, syncId: string): Promise<GitOpsSyncStatus> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/gitops/syncs/${syncId}/status`));
	}

	async browseFiles(environmentId: string, syncId: string, path?: string): Promise<GitOpsBrowseResponse> {
		const params = path ? { path } : {};
		return this.handleResponse(this.api.get(`/environments/${environmentId}/gitops/syncs/${syncId}/files`, { params }));
	}

	async importSyncs(environmentId: string, syncs: ImportGitOpsSyncRequest[]): Promise<ImportGitOpsSyncResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/gitops/syncs/import`, syncs));
	}

	async previewBackup(environmentId: string, syncId: string): Promise<GitOpsBackupPreview> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/gitops/syncs/${syncId}/backup/preview`));
	}

	async getBackupHistory(environmentId: string, syncId: string, limit = 20): Promise<GitOpsBackupHistoryResponse> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/gitops/syncs/${syncId}/backup/history`, { params: { limit } })
		);
	}

	async getBackupRevision(environmentId: string, syncId: string, commit: string): Promise<GitOpsBackupRevision> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/gitops/syncs/${syncId}/backup/history/${encodeURIComponent(commit)}`)
		);
	}

	async resolveBackupConflict(
		environmentId: string,
		syncId: string,
		request: GitOpsResolveBackupConflictRequest
	): Promise<GitOpsSyncResult> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/gitops/syncs/${syncId}/backup/resolve`, request));
	}
}

export const gitOpsSyncService = new GitOpsSyncService();
