import BaseAPIService from './api-service';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
import type {
	CreateSystemBackup,
	SystemBackupPolicyCollection,
	SystemBackupRecoveryKey,
	SystemBackupRun,
	UpdateSystemBackupPolicy
} from '#lib/types/system-backup';
import { transformPaginationParams } from '#lib/utils/tables';
import type { BackupFileBrowseRequest, BackupFileEntry, BackupRestoreSelection } from '#lib/types/backup';

class SystemBackupService extends BaseAPIService {
	async list(options?: SearchPaginationSortRequest): Promise<Paginated<SystemBackupRun>> {
		const response = await this.api.get('/backups', { params: transformPaginationParams(options) });
		return response.data;
	}

	async getPolicies(): Promise<SystemBackupPolicyCollection> {
		return this.handleResponse(this.api.get('/backups/policies'));
	}

	async updatePolicies(policies: UpdateSystemBackupPolicy[]): Promise<SystemBackupPolicyCollection> {
		return this.handleResponse(this.api.put('/backups/policies', { policies }));
	}

	async generateRecoveryKey(): Promise<SystemBackupRecoveryKey> {
		return this.handleResponse(this.api.post('/backups/recovery-key/generate'));
	}

	async setRecoveryKey(recoveryKey: string): Promise<{ configured: boolean }> {
		return this.handleResponse(this.api.put('/backups/recovery-key', { recoveryKey }));
	}

	async create(input: CreateSystemBackup): Promise<SystemBackupRun> {
		return this.handleResponse(this.api.post('/backups', input));
	}

	async restore(id: string, recoveryKey: string): Promise<void> {
		await this.handleResponse(this.api.post(`/backups/${id}/restore`, { recoveryKey }));
	}

	async browseFiles(id: string, recoveryKey: string, request: BackupFileBrowseRequest): Promise<Paginated<BackupFileEntry>> {
		const response = await this.api.post(`/backups/${id}/files/browse`, { recoveryKey }, { params: request });
		return response.data;
	}

	async restoreFiles(id: string, recoveryKey: string, selection: BackupRestoreSelection): Promise<unknown> {
		return this.handleResponse(this.api.post(`/backups/${id}/restore-files`, { recoveryKey, ...selection }));
	}

	async upload(id: string, s3DestinationId: string, recoveryKey: string): Promise<SystemBackupRun> {
		return this.handleResponse(this.api.post(`/backups/${id}/upload`, { s3DestinationId, recoveryKey }));
	}

	async delete(id: string, recoveryKey: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/backups/${id}`, { data: { recoveryKey } }));
	}

	async discover(s3DestinationId: string, recoveryKey: string): Promise<number> {
		return this.handleResponse(this.api.post('/backups/discover', { s3DestinationId, recoveryKey }));
	}
}

export const systemBackupService = new SystemBackupService();
