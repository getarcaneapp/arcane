import BaseAPIService from './api-service';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
import type {
	CreateSystemBackup,
	SystemBackupPolicyCollection,
	SystemBackupRun,
	UpdateSystemBackupPolicy
} from '$lib/types/system-backup';
import { transformPaginationParams } from '$lib/utils/tables';

class SystemBackupService extends BaseAPIService {
	async list(options?: SearchPaginationSortRequest): Promise<Paginated<SystemBackupRun>> {
		const response = await this.api.get('/system-backups', { params: transformPaginationParams(options) });
		return response.data;
	}

	async getPolicies(): Promise<SystemBackupPolicyCollection> {
		return this.handleResponse(this.api.get('/system-backups/policies'));
	}

	async updatePolicies(policies: UpdateSystemBackupPolicy[]): Promise<SystemBackupPolicyCollection> {
		return this.handleResponse(this.api.put('/system-backups/policies', { policies }));
	}

	async setRecoveryKey(recoveryKey: string): Promise<{ configured: boolean }> {
		return this.handleResponse(this.api.put('/system-backups/recovery-key', { recoveryKey }));
	}

	async create(input: CreateSystemBackup): Promise<SystemBackupRun> {
		return this.handleResponse(this.api.post('/system-backups', input));
	}

	async restore(id: string, recoveryKey: string): Promise<void> {
		await this.handleResponse(this.api.post(`/system-backups/${id}/restore`, { recoveryKey }));
	}

	async upload(id: string, s3DestinationId: string, recoveryKey: string): Promise<SystemBackupRun> {
		return this.handleResponse(this.api.post(`/system-backups/${id}/upload`, { s3DestinationId, recoveryKey }));
	}

	async delete(id: string, recoveryKey: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/system-backups/${id}`, { data: { recoveryKey } }));
	}

	async discover(s3DestinationId: string, recoveryKey: string): Promise<number> {
		return this.handleResponse(this.api.post('/system-backups/discover', { s3DestinationId, recoveryKey }));
	}
}

export const systemBackupService = new SystemBackupService();
