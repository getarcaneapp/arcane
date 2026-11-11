import type {
	BackupFileEntry,
	BackupRestoreSelection,
	CreateSystemBackupRequest,
	SystemVolumeBackupHistoryEntry,
	SystemBackupPolicyCollection,
	SystemBackupRecoveryKey,
	SystemBackupRun,
	UpdateBackupPolicy,
	SystemVolumeBackupPolicyCollection,
	UpdateSystemVolumeBackupPolicy,
	SystemVolumeBackupOption,
	BackupRunAccepted,
	RunSystemVolumeBackupsRequest
} from '#lib/types/backup.js';
import type { MessageResponse, Paginated } from '#lib/types/base.js';
import type { BackupFileBrowseRequest } from '#lib/types/ui/backup.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class SystemBackupService extends BaseAPIService {
	async list(options?: SearchPaginationSortRequest): Promise<Paginated<SystemBackupRun>> {
		const response = await this.api.get('/backups', { params: transformPaginationParams(options) });
		return response.data;
	}

	async listManagerHistory(
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<Paginated<SystemVolumeBackupHistoryEntry>> {
		const response = await this.api.get('/backups/history', { ...config, params: transformPaginationParams(options) });
		return response.data;
	}

	async listHistory(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<Paginated<SystemVolumeBackupHistoryEntry>> {
		const response = await this.api.get(`/agents/${environmentId}/backups/history`, {
			...config,
			params: transformPaginationParams(options)
		});
		return response.data;
	}

	async getSystemVolumeConfig(): Promise<SystemVolumeBackupPolicyCollection> {
		return this.handleResponse(this.api.get('/backups/volumes/config'));
	}

	async updateSystemVolumeConfig(policies: UpdateSystemVolumeBackupPolicy[]): Promise<SystemVolumeBackupPolicyCollection> {
		return this.handleResponse(this.api.put('/backups/volumes/config', { policies }));
	}

	async listSystemVolumeOptions(): Promise<SystemVolumeBackupOption[]> {
		return this.handleResponse(this.api.get('/backups/volumes/options'));
	}

	async runSystemVolumeBackups(request: RunSystemVolumeBackupsRequest = {}): Promise<BackupRunAccepted> {
		return this.handleResponse(this.api.post('/backups/volumes/run', request));
	}

	async getPolicies(): Promise<SystemBackupPolicyCollection> {
		return this.handleResponse(this.api.get('/backups/policies'));
	}

	async updatePolicies(policies: UpdateBackupPolicy[]): Promise<SystemBackupPolicyCollection> {
		return this.handleResponse(this.api.put('/backups/policies', { policies }));
	}

	async generateRecoveryKey(): Promise<SystemBackupRecoveryKey> {
		return this.handleResponse(this.api.post('/backups/recovery/key', undefined, { params: { action: 'generate' } }));
	}

	async setRecoveryKey(recoveryKey: string): Promise<{ configured: boolean }> {
		return this.handleResponse(this.api.put('/backups/recovery/key', { recoveryKey }));
	}

	async create(input: CreateSystemBackupRequest): Promise<SystemBackupRun> {
		return this.handleResponse(this.api.post('/backups', input));
	}

	async restore(environmentId: string, id: string, recoveryKey: string): Promise<void> {
		await this.handleResponse(
			this.api.post(`/agents/${environmentId}/backups/${id}`, { recoveryKey }, { params: { action: 'restore' } })
		);
	}

	async browseFiles(
		environmentId: string,
		id: string,
		recoveryKey: string,
		request: BackupFileBrowseRequest
	): Promise<Paginated<BackupFileEntry>> {
		const response = await this.api.post(
			`/agents/${environmentId}/backups/${id}/files/browse`,
			{ recoveryKey },
			{ params: request }
		);
		return response.data;
	}

	async restoreFiles(
		environmentId: string,
		id: string,
		recoveryKey: string,
		selection: BackupRestoreSelection
	): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(
				`/agents/${environmentId}/backups/${id}`,
				{ recoveryKey, ...selection },
				{ params: { action: 'restorefiles' } }
			)
		);
	}

	async upload(environmentId: string, id: string, s3DestinationId: string, recoveryKey: string): Promise<SystemBackupRun> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/backups/${id}/upload`, { s3DestinationId, recoveryKey }));
	}

	async delete(environmentId: string, id: string, recoveryKey: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/agents/${environmentId}/backups/${id}`, { data: { recoveryKey } }));
	}

	async discover(s3DestinationId: string, recoveryKey: string): Promise<number> {
		return this.handleResponse(this.api.post('/backups/discover', { s3DestinationId, recoveryKey }));
	}
}

export const systemBackupService = new SystemBackupService();
