import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type {
	ImageSummaryDto,
	ImageUsageCounts,
	ImageUpdateInfoDto,
	ImageBuildRecord,
	ImageAttestationListDto,
	ImageAttestationRequestOptions,
	ImageHistoryItemDto,
	ImageSearchResultDto,
	ImageTagRequest
} from '$lib/types/docker';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/shared';
import type { AutoUpdateCheck, AutoUpdateResult } from '$lib/types/automation';
import type { PruneImagesOptions } from '$lib/types/automation';
import { transformPaginationParams } from '$lib/utils/tables';
import { readNdjsonStream } from '$lib/utils/streaming';
import { m } from '$lib/paraglide/messages';

export type ImagePullResult = {
	success: boolean;
	imageName: string;
	error?: string;
};

class ImageService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getImages(options?: SearchPaginationSortRequest): Promise<Paginated<ImageSummaryDto>> {
		const envId = await this.resolveEnvironmentId();
		return this.getImagesForEnvironment(envId, options);
	}

	async getImagesForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest
	): Promise<Paginated<ImageSummaryDto>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/images`, { params });
		return res.data;
	}

	async getImageUsageCounts(): Promise<ImageUsageCounts> {
		const envId = await this.resolveEnvironmentId();
		return this.getImageUsageCountsForEnvironment(envId);
	}

	async getImageUsageCountsForEnvironment(environmentId: string): Promise<ImageUsageCounts> {
		const res = await this.api.get(`/environments/${environmentId}/images/counts`);
		return res.data.data;
	}

	async getImage(imageId: string): Promise<any> {
		const envId = await this.resolveEnvironmentId();
		return this.getImageForEnvironment(envId, imageId);
	}

	async getImageForEnvironment(environmentId: string, imageId: string): Promise<any> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/images/${imageId}`));
	}

	async getImageAttestationsForEnvironment(
		environmentId: string,
		imageId: string,
		options?: ImageAttestationRequestOptions
	): Promise<ImageAttestationListDto> {
		const params: Record<string, string> = {};
		if (options?.platform) params['platform'] = options.platform;
		if (options?.predicateType) params['predicateType'] = options.predicateType;
		if (options?.statement) params['statement'] = 'true';

		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/images/${imageId}/attestations`, {
				params
			})
		);
	}

	async tagImage(imageId: string, request: ImageTagRequest): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/images/${encodeURIComponent(imageId)}/tag`, request));
	}

	async getImageHistory(imageId: string): Promise<ImageHistoryItemDto[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/images/${encodeURIComponent(imageId)}/history`));
	}

	async searchImages(term: string): Promise<ImageSearchResultDto[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/images/search`, { params: { term } }));
	}

	async getImageExportUrl(imageId: string): Promise<string> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return `/api/environments/${envId}/images/${encodeURIComponent(imageId)}/export`;
	}

	async pullImageStream(imageName: string, onLine?: (data: any) => void): Promise<ImagePullResult> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		try {
			const response = await fetch(`/api/environments/${envId}/images/pull`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ imageName })
			});

			if (!response.ok || !response.body) {
				const errorData = await response.json().catch(() => ({
					data: { message: m.images_pull_server_error() }
				}));
				const errorMessage =
					errorData.data?.message ||
					errorData.error ||
					errorData.message ||
					`${m.images_pull_server_error()}: HTTP ${response.status}`;
				throw new Error(errorMessage);
			}

			await readNdjsonStream(response.body, (parsed) => {
				onLine?.(parsed);
				if (parsed?.error) {
					const message = typeof parsed.error === 'string' ? parsed.error : parsed.error.message || m.images_pull_stream_failed();
					throw new Error(message);
				}
			});
			return { success: true, imageName };
		} catch (error) {
			return {
				success: false,
				imageName,
				error: error instanceof Error ? error.message : m.images_pull_stream_failed()
			};
		}
	}

	async pullImage(imageName: string, tag: string = 'latest', auth?: any): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/images/pull`, { imageName, tag, auth }));
	}

	async deleteImage(imageId: string, options?: { force?: boolean; noprune?: boolean }): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.delete(`/environments/${envId}/images/${imageId}`, { params: options }));
	}

	async pruneImages(options: PruneImagesOptions): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/images/prune`, options));
	}

	async checkImageUpdateByID(imageId: string): Promise<ImageUpdateInfoDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/image-updates/check/${imageId}`, {}));
	}

	async checkAllImages(): Promise<Record<string, ImageUpdateInfoDto>> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/image-updates/check-all`, {}));
	}

	async checkMultipleImages(imageRefs: string[]): Promise<Record<string, ImageUpdateInfoDto>> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/image-updates/check-batch`, { imageRefs }));
	}

	async getUpdateInfoByRefs(imageRefs: string[]): Promise<Record<string, ImageUpdateInfoDto>> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		if (imageRefs.length === 0) {
			return {};
		}

		return this.handleResponse(
			this.api.get(`/environments/${envId}/image-updates/by-refs`, {
				params: { imageRefs: imageRefs.join(',') }
			})
		);
	}

	async runAutoUpdate(options?: AutoUpdateCheck): Promise<AutoUpdateResult> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/updater/run`, options));
	}

	async uploadImage(file: File): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const formData = new FormData();
		formData.append('file', file);
		return this.handleResponse(this.api.post(`/environments/${envId}/images/upload`, formData));
	}

	async getImageBuilds(options?: SearchPaginationSortRequest): Promise<Paginated<ImageBuildRecord>> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/images/builds`, { params });
		return res.data;
	}

	async getImageBuild(buildId: string): Promise<ImageBuildRecord> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/images/builds/${buildId}`));
	}
}

export const imageService = new ImageService();
