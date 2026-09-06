import BaseAPIService from './api-service';
import type { CreateS3Destination, S3Destination, UpdateS3Destination } from '#lib/types/s3-destination.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

class S3DestinationService extends BaseAPIService {
	async list(options?: SearchPaginationSortRequest): Promise<Paginated<S3Destination>> {
		const params = transformPaginationParams(options);
		const response = await this.api.get('/backups/s3', { params });
		return response.data;
	}

	async listAll(): Promise<S3Destination[]> {
		return this.handleResponse(this.api.get('/backups/s3/options'));
	}

	async create(destination: CreateS3Destination): Promise<S3Destination> {
		return this.handleResponse(this.api.post('/backups/s3', destination));
	}

	async update(id: string, destination: UpdateS3Destination): Promise<S3Destination> {
		return this.handleResponse(this.api.put(`/backups/s3/${id}`, destination));
	}

	async test(id: string, destination?: UpdateS3Destination): Promise<void> {
		await this.handleResponse(this.api.post(`/backups/s3/${id}/test`, destination));
	}

	async testConfiguration(destination: CreateS3Destination): Promise<void> {
		await this.handleResponse(this.api.post('/backups/s3/test', destination));
	}

	async delete(id: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/backups/s3/${id}`));
	}
}

export const s3DestinationService = new S3DestinationService();
