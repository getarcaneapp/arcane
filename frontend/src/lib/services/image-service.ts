import { m } from '#lib/paraglide/messages.js';
import type { Paginated, MessageResponse } from '#lib/types/base.js';
import type { ContainerRegistryCredential } from '#lib/types/containerregistry.js';
import type {
	ImageListResponse,
	ImageBuildRequest,
	ImageDetailSummary,
	ImageLoadResult,
	ImagePruneReport,
	ImageUsageCounts,
	ImageBuildRecord,
	ImageAttestationList,
	ImageReleaseNotes,
	ImageHistoryItem,
	ImageSearchResult,
	ImageTagRequest
} from '#lib/types/image.js';
import type { PatchOptions, PatchRecord, PatchTarget } from '#lib/types/imagepatch.js';
import type { ImageUpdateResponse } from '#lib/types/imageupdate.js';
import type { OperationStreamFrame } from '#lib/types/stream.js';
import type { PruneImagesOptions } from '#lib/types/system.js';
import type { ImageAttestationRequestOptions, ImagePullResult } from '#lib/types/ui/image.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { UploadProgressCallback } from '#lib/types/ui/upload.js';
import type { UpdaterOptions, UpdaterResult } from '#lib/types/updater.js';
import { readNdjsonStream } from '#lib/utils/streaming.js';
import { transformPaginationParams } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import BaseAPIService, {
	type APIRequestConfig,
	extractServerMessage,
	isSessionCancelledError,
	withSessionSignal
} from './api-service';
import { uploadService } from './upload-service';

class ImageService extends BaseAPIService {
	async getImagesForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<ImageListResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/agents/${environmentId}/images`, { ...config, params });
		return res.data;
	}

	async getImageUsageCountsForEnvironment(environmentId: string, config?: APIRequestConfig): Promise<ImageUsageCounts> {
		const res = await this.api.get(`/agents/${environmentId}/images/counts`, config);
		return res.data.data;
	}

	async getImageForEnvironment(environmentId: string, imageId: string): Promise<ImageDetailSummary> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/images/${imageId}`));
	}

	async getImageAttestationsForEnvironment(
		environmentId: string,
		imageId: string,
		options?: ImageAttestationRequestOptions
	): Promise<ImageAttestationList> {
		const params: Record<string, string> = {};
		if (options?.platform) params['platform'] = options.platform;
		if (options?.predicateType) params['predicateType'] = options.predicateType;
		if (options?.statement) params['statement'] = 'true';

		return this.handleResponse(
			this.api.get(`/agents/${environmentId}/images/${imageId}/attestations`, {
				params
			})
		);
	}

	async getImageReleaseNotes(
		environmentId: string,
		imageRef: string,
		options?: { version?: string; digest?: string }
	): Promise<ImageReleaseNotes> {
		const params: Record<string, string> = { imageRef };
		if (options?.version) params['version'] = options.version;
		if (options?.digest) params['digest'] = options.digest;

		return this.handleResponse(this.api.get(`/agents/${environmentId}/images/updates/releasenotes`, { params }));
	}

	async tagImage(imageId: string, request: ImageTagRequest, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/images/${encodeURIComponent(imageId)}/tag`, request));
	}

	async getImageHistory(imageId: string, environmentId: string): Promise<ImageHistoryItem[]> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/images/${encodeURIComponent(imageId)}/history`));
	}

	async searchImages(term: string, environmentId: string): Promise<ImageSearchResult[]> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/images/search`, { params: { term } }));
	}

	async getImageExportUrl(imageId: string, environmentId: string): Promise<string> {
		return `/api/agents/${environmentId}/images/${encodeURIComponent(imageId)}/export`;
	}

	async pullImageStream(
		imageName: string,
		onLine: ((data: unknown) => void) | undefined,
		environmentId: string
	): Promise<ImagePullResult> {
		const operationResult = await tryCatch(
			(async () => {
				const body = await this.postStreamInternal(
					`/api/agents/${environmentId}/images/pull`,
					{ imageName },
					(status) => `${m.images_pull_server_error()}: HTTP ${status}`
				);
				await readNdjsonStream(body, (parsed: OperationStreamFrame) => {
					onLine?.(parsed);
					if (parsed?.error) {
						throw new Error(
							typeof parsed.error === 'string' ? parsed.error : parsed.error.message || m.images_pull_stream_failed()
						);
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

	async buildImageStream(request: ImageBuildRequest, environmentId: string, onLine: (line: string) => boolean): Promise<void> {
		const body = await this.postStreamInternal(`/api/agents/${environmentId}/images/build`, request, (status) =>
			m.build_request_failed_http({ status })
		);
		const reader = body.getReader();
		const decoder = new TextDecoder();
		let buffer = '';
		let streamComplete = false;

		while (!streamComplete) {
			const { done, value } = await reader.read();
			if (done) {
				break;
			}

			buffer += decoder.decode(value, { stream: true });
			const lines = buffer.split('\n');
			buffer = lines.pop() || '';

			streamComplete = lines.some(onLine);
		}
		if (streamComplete) {
			await tryCatch(reader.cancel());
		}
	}

	private async postStreamInternal(
		url: string,
		body: unknown,
		failedMessage: (status: number) => string
	): Promise<ReadableStream<Uint8Array>> {
		const response = await fetch(url, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(body),
			signal: withSessionSignal()
		});
		if (response.ok && response.body) {
			return response.body;
		}
		const errorBody = await tryCatch(response.json());
		throw new Error(extractServerMessage(errorBody.data, true) ?? failedMessage(response.status));
	}

	async pullImage(
		imageName: string,
		tag: string = 'latest',
		auth: ContainerRegistryCredential | undefined,
		environmentId: string
	): Promise<string> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/images/pull`, { imageName, tag, auth }));
	}

	async deleteImage(
		imageId: string,
		options: { force?: boolean; noprune?: boolean } | undefined,
		environmentId: string
	): Promise<MessageResponse> {
		return this.handleResponse(this.api.delete(`/agents/${environmentId}/images/${imageId}`, { params: options }));
	}

	async pruneImages(options: PruneImagesOptions, environmentId: string): Promise<ImagePruneReport> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/images/prune`, options));
	}

	async patchImage(imageId: string, options: PatchOptions | undefined, environmentId: string): Promise<PatchRecord> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/images/${encodeURIComponent(imageId)}/patch`, options ?? {})
		);
	}

	async listPatchTargets(
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<Paginated<PatchTarget>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/agents/${environmentId}/images/patches/targets`, { ...config, params });
		return res.data;
	}

	async checkImageUpdateByID(imageId: string, environmentId: string): Promise<ImageUpdateResponse> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/images/updates/check/${imageId}`, {}));
	}

	async checkAllImages(environmentId: string): Promise<Record<string, ImageUpdateResponse>> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/images/updates/check`, {}, { params: { action: 'all' } }));
	}

	async checkMultipleImages(imageRefs: string[], environmentId: string): Promise<Record<string, ImageUpdateResponse>> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/images/updates/check`, { imageRefs }, { params: { action: 'batch' } })
		);
	}

	async runAutoUpdate(options: UpdaterOptions | undefined, environmentId: string): Promise<UpdaterResult> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/updater/run`, options));
	}

	async uploadImage(file: File, environmentId: string, onProgress?: UploadProgressCallback): Promise<ImageLoadResult> {
		const uploadId = await uploadService.uploadFile(environmentId, 'image', file, onProgress);
		return this.handleResponse(this.api.post(`/agents/${environmentId}/images/upload`, { uploadId }));
	}

	async getImageBuilds(
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<Paginated<ImageBuildRecord>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/agents/${environmentId}/images/builds`, { ...config, params });
		return res.data;
	}

	async getImageBuild(buildId: string, environmentId: string): Promise<ImageBuildRecord> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/images/builds/${buildId}`));
	}
}

export const imageService = new ImageService();
