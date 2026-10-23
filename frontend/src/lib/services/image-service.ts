import { m } from '#lib/paraglide/messages.js';
import type { AutoUpdateCheck, AutoUpdateResult } from '#lib/types/automation.js';
import type { PruneImagesOptions } from '#lib/types/automation.js';
import type {
	ImageSummaryDto,
	ImageUsageCounts,
	ImageUpdateInfoDto,
	ImageBuildRecord,
	ImageAttestationListDto,
	ImageAttestationRequestOptions,
	ImageHistoryItemDto,
	ImageSearchResultDto,
	ImageTagRequest,
	ImagePatchOptions,
	ImagePatchRecordDto,
	ImagePatchTargetDto
} from '#lib/types/docker.js';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import { readNdjsonStream } from '#lib/utils/streaming.js';
import { transformPaginationParams } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import BaseAPIService, { type APIRequestConfig, isSessionCancelledError, withSessionSignal } from './api-service';
import { uploadService, type UploadProgressCallback } from './upload-service';

export type ImagePullResult = {
	success: boolean;
	imageName: string;
	error?: string;
	/** The pull was abandoned because the user signed out. */
	cancelled?: boolean;
};

/** Image list page plus the upload limit (MB) the environment enforces; absent on older agents. */
export type ImagesPaginatedResponse = Paginated<ImageSummaryDto> & {
	maxImageUploadSize?: number;
};

class ImageService extends BaseAPIService {
	async getImagesForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<ImagesPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/images`, { ...config, params });
		return res.data;
	}

	async getImageUsageCountsForEnvironment(environmentId: string, config?: APIRequestConfig): Promise<ImageUsageCounts> {
		const res = await this.api.get(`/environments/${environmentId}/images/counts`, config);
		return res.data.data;
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

	async tagImage(imageId: string, request: ImageTagRequest, environmentId: string): Promise<any> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/images/${encodeURIComponent(imageId)}/tag`, request)
		);
	}

	async getImageHistory(imageId: string, environmentId: string): Promise<ImageHistoryItemDto[]> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/images/${encodeURIComponent(imageId)}/history`));
	}

	async searchImages(term: string, environmentId: string): Promise<ImageSearchResultDto[]> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/images/search`, { params: { term } }));
	}

	async getImageExportUrl(imageId: string, environmentId: string): Promise<string> {
		return `/api/environments/${environmentId}/images/${encodeURIComponent(imageId)}/export`;
	}

	async pullImageStream(
		imageName: string,
		onLine: ((data: any) => void) | undefined,
		environmentId: string
	): Promise<ImagePullResult> {
		const operationResult = await tryCatch(
			(async () => {
				const response = await fetch(`/api/environments/${environmentId}/images/pull`, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ imageName }),
					signal: withSessionSignal()
				});

				if (!response.ok || !response.body) {
					const errorData = await tryCatch(response.json()).then((result) => {
						if (result.error !== null) {
							return {
								data: { message: m.images_pull_server_error() }
							};
						} else {
							return result.data;
						}
					});
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
						const message =
							typeof parsed.error === 'string' ? parsed.error : parsed.error.message || m.images_pull_stream_failed();
						throw new Error(message);
					}
					// The terminal frame marks success; don't wait on the network EOF.
					return parsed?.done === true;
				});
				return { success: true, imageName };
			})()
		);
		if (operationResult.error !== null) {
			const error = operationResult.error;
			if (isSessionCancelledError(error)) {
				return { success: false, imageName, cancelled: true };
			}
			return {
				success: false,
				imageName,
				error: error instanceof Error ? error.message : m.images_pull_stream_failed()
			};
		} else {
			return operationResult.data;
		}
	}

	async pullImage(imageName: string, tag: string = 'latest', auth: any, environmentId: string): Promise<any> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/images/pull`, { imageName, tag, auth }));
	}

	async deleteImage(
		imageId: string,
		options: { force?: boolean; noprune?: boolean } | undefined,
		environmentId: string
	): Promise<any> {
		return this.handleResponse(this.api.delete(`/environments/${environmentId}/images/${imageId}`, { params: options }));
	}

	async pruneImages(options: PruneImagesOptions, environmentId: string): Promise<any> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/images/prune`, options));
	}

	async patchImage(imageId: string, options: ImagePatchOptions | undefined, environmentId: string): Promise<ImagePatchRecordDto> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/images/${encodeURIComponent(imageId)}/patch`, options ?? {})
		);
	}

	async listPatchTargets(
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<Paginated<ImagePatchTargetDto>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/images/patches/targets`, { ...config, params });
		return res.data;
	}

	async checkImageUpdateByID(imageId: string, environmentId: string): Promise<ImageUpdateInfoDto> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/images/updates/check/${imageId}`, {}));
	}

	async checkAllImages(environmentId: string): Promise<Record<string, ImageUpdateInfoDto>> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/images/updates/check`, {}, { params: { action: 'all' } })
		);
	}

	async checkMultipleImages(imageRefs: string[], environmentId: string): Promise<Record<string, ImageUpdateInfoDto>> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/images/updates/check`, { imageRefs }, { params: { action: 'batch' } })
		);
	}

	async runAutoUpdate(options: AutoUpdateCheck | undefined, environmentId: string): Promise<AutoUpdateResult> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/updater/run`, options));
	}

	async uploadImage(file: File, environmentId: string, onProgress?: UploadProgressCallback): Promise<any> {
		const uploadId = await uploadService.uploadFile(environmentId, 'image', file, onProgress);
		return this.handleResponse(this.api.post(`/environments/${environmentId}/images/upload`, { uploadId }));
	}

	async getImageBuilds(
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<Paginated<ImageBuildRecord>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/images/builds`, { ...config, params });
		return res.data;
	}

	async getImageBuild(buildId: string, environmentId: string): Promise<ImageBuildRecord> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/images/builds/${buildId}`));
	}
}

export const imageService = new ImageService();
