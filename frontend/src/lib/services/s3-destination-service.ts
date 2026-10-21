import type { S3Destination, S3DestinationCreate, S3DestinationUpdate } from '#lib/types/s3-destination.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class S3DestinationService extends BaseAPIService {
	async list(options?: SearchPaginationSortRequest): Promise<Paginated<S3Destination>> {
		const params = transformPaginationParams(options);
		const response = await this.api.get('/backups/s3', { params });
		return response.data;
	}

	async listAll(): Promise<S3Destination[]> {
		return this.handleResponse(this.api.get('/backups/s3/options'));
	}

	async create(destination: S3DestinationCreate): Promise<S3Destination> {
		return this.handleResponse(this.api.post('/backups/s3', destination));
	}

	async update(id: string, destination: S3DestinationUpdate): Promise<S3Destination> {
		return this.handleResponse(this.api.patch(`/backups/s3/${id}`, destination));
	}

	async test(id: string, destination?: S3DestinationCreate): Promise<void> {
		await this.handleResponse(this.api.post(`/backups/s3/${id}`, destination, { params: { action: 'test' } }));
	}

	async testConfiguration(destination: S3DestinationCreate): Promise<void> {
		await this.handleResponse(this.api.post('/backups/s3/test', destination));
	}

	async delete(id: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/backups/s3/${id}`));
	}
}

export const s3DestinationService = new S3DestinationService();
