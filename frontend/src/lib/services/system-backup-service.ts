import BaseAPIService from './api-service';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
import type {
	CreateSystemBackup,
	BackupHistoryEntry,
	SystemBackupPolicyCollection,
	SystemBackupRecoveryKey,
	SystemBackupRun,
	UpdateSystemBackupPolicy,
	SystemVolumeBackupConfig,
	SystemVolumeBackupOption,
	SystemVolumeBackupRunResult
} from '#lib/types/system-backup';
import { transformPaginationParams } from '#lib/utils/tables';

class SystemBackupService extends BaseAPIService {
	async list(options?: SearchPaginationSortRequest): Promise<Paginated<SystemBackupRun>> {
		const response = await this.api.get('/backups', { params: transformPaginationParams(options) });
		return response.data;
	}

	async listHistory(options?: SearchPaginationSortRequest): Promise<Paginated<BackupHistoryEntry>> {
		const response = await this.api.get('/backups/history', { params: transformPaginationParams(options) });
		return response.data;
	}

	async getSystemVolumeConfig(): Promise<SystemVolumeBackupConfig> {
		return this.handleResponse(this.api.get('/backups/volumes/config'));
	}

	async updateSystemVolumeConfig(config: SystemVolumeBackupConfig): Promise<SystemVolumeBackupConfig> {
		return this.handleResponse(this.api.put('/backups/volumes/config', config));
	}

	async listSystemVolumeOptions(): Promise<SystemVolumeBackupOption[]> {
		return this.handleResponse(this.api.get('/backups/volumes/options'));
	}

	async runSystemVolumeBackups(): Promise<SystemVolumeBackupRunResult> {
		return this.handleResponse(this.api.post('/backups/volumes/run'));
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
