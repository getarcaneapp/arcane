import type { AuditExportFormat, AuditLog } from '#lib/types/auth.js';
import type { FilterMap, Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { filenameFromContentDisposition } from '#lib/utils/browser-download.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class AuditAPIService extends BaseAPIService {
	async getLogs(options?: SearchPaginationSortRequest): Promise<Paginated<AuditLog>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/audit/logs', { params });
		return res.data;
	}

	async get(id: string): Promise<AuditLog> {
		return this.handleResponse(this.api.get(`/audit/logs/${id}`));
	}

	async export(format: AuditExportFormat, filters?: FilterMap): Promise<{ blob: Blob; filename: string }> {
		const params = { ...transformPaginationParams({ filters }), format };
		const res = await this.api.get('/audit/logs/export', { params, responseType: 'blob' });
		return {
			blob: res.data,
			filename: filenameFromContentDisposition(res.headers.get('content-disposition'), `audit-logs.${format}`)
		};
	}
}

export const auditService = new AuditAPIService();
