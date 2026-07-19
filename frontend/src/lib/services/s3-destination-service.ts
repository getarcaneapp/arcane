import BaseAPIService from './api-service';
import type { CreateS3Destination, S3Destination, UpdateS3Destination } from '$lib/types/s3-destination';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
import { transformPaginationParams } from '$lib/utils/tables';

class S3DestinationService extends BaseAPIService {
	async list(options?: SearchPaginationSortRequest): Promise<Paginated<S3Destination>> {
		const params = transformPaginationParams(options);
		const response = await this.api.get('/s3-destinations', { params });
		return response.data;
	}

	async listAll(): Promise<S3Destination[]> {
		return this.handleResponse(this.api.get('/s3-destinations/options'));
	}

	async create(destination: CreateS3Destination): Promise<S3Destination> {
		return this.handleResponse(this.api.post('/s3-destinations', destination));
	}

	async update(id: string, destination: UpdateS3Destination): Promise<S3Destination> {
		return this.handleResponse(this.api.put(`/s3-destinations/${id}`, destination));
	}

	async delete(id: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/s3-destinations/${id}`));
	}
}

export const s3DestinationService = new S3DestinationService();
