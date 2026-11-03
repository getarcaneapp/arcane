import type { Paginated } from '#lib/types/base.js';
import type {
	KubeDownReport,
	KubeDownRequest,
	KubeGenerateRequest,
	KubeGenerateResponse,
	KubePlayReport,
	KubePlayRequest,
	Pod,
	PodActionRequest,
	PodCreateRequest,
	PodCreateResponse,
	PodInspect,
	PodPruneReport,
	PodTop,
	PodmanAutoUpdateOptions,
	PodmanAutoUpdateResult,
	PodmanInfo,
	PodmanSecret,
	PodmanSecretCreateRequest,
	PodmanSecretCreateResponse,
	PodmanSecretUpdateRequest,
	PodmanStatus,
	PodmanUnitStatus,
	QuadletDetail,
	QuadletFile,
	QuadletInstallReport,
	QuadletInstallRequest,
	QuadletRemoveReport,
	QuadletUpdateRequest
} from '#lib/types/podman.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { PodmanPodAction, PodmanQuadletAction } from '#lib/types/ui/podman.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class PodmanService extends BaseAPIService {
	async getStatus(environmentId: string, config?: APIRequestConfig): Promise<PodmanStatus> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/podman/status`, config));
	}

	async getInfo(environmentId: string, config?: APIRequestConfig): Promise<PodmanInfo> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/podman/info`, config));
	}

	async runAutoUpdate(options: PodmanAutoUpdateOptions = {}, environmentId: string): Promise<PodmanAutoUpdateResult> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/podman/autoupdate`, options));
	}

	async getPods(scope: string, options?: SearchPaginationSortRequest, config?: APIRequestConfig): Promise<Paginated<Pod>> {
		const res = await this.api.get(`/environments/${scope}/podman/pods`, {
			...config,
			params: transformPaginationParams(options)
		});
		return res.data;
	}

	async getPod(podId: string, environmentId: string): Promise<PodInspect> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/podman/pods/${encodeURIComponent(podId)}`));
	}

	async getPodTop(podId: string, environmentId: string): Promise<PodTop> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/podman/pods/${encodeURIComponent(podId)}/top`));
	}

	async createPod(request: PodCreateRequest, environmentId: string): Promise<PodCreateResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/podman/pods`, request));
	}

	async prunePods(environmentId: string): Promise<PodPruneReport[]> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/podman/pods/prune`, {}));
	}

	async podAction(podId: string, action: PodmanPodAction, environmentId: string, request: PodActionRequest = {}): Promise<void> {
		await this.handleResponse(
			this.api.post(`/environments/${environmentId}/podman/pods/${encodeURIComponent(podId)}`, request, { params: { action } })
		);
	}

	async removePod(podId: string, force = false, environmentId: string): Promise<void> {
		await this.handleResponse(
			this.api.delete(`/environments/${environmentId}/podman/pods/${encodeURIComponent(podId)}`, { params: { force } })
		);
	}

	async getQuadlets(
		scope: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<Paginated<QuadletDetail>> {
		const res = await this.api.get(`/environments/${scope}/podman/quadlets`, {
			...config,
			params: transformPaginationParams(options)
		});
		return res.data;
	}

	async getQuadlet(name: string, environmentId: string): Promise<QuadletDetail> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/podman/quadlets/${encodeURIComponent(name)}`));
	}

	async getQuadletFile(name: string, environmentId: string): Promise<QuadletFile> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/podman/quadlets/${encodeURIComponent(name)}/file`));
	}

	async installQuadlet(request: QuadletInstallRequest, environmentId: string): Promise<QuadletInstallReport> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/podman/quadlets`, request));
	}

	async updateQuadlet(name: string, request: QuadletUpdateRequest, environmentId: string): Promise<QuadletInstallReport> {
		return this.handleResponse(
			this.api.put(`/environments/${environmentId}/podman/quadlets/${encodeURIComponent(name)}`, request)
		);
	}

	async removeQuadlet(name: string, environmentId: string): Promise<QuadletRemoveReport> {
		return this.handleResponse(this.api.delete(`/environments/${environmentId}/podman/quadlets/${encodeURIComponent(name)}`));
	}

	async quadletAction(name: string, action: PodmanQuadletAction, environmentId: string): Promise<PodmanUnitStatus> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/podman/quadlets/${encodeURIComponent(name)}`, {}, { params: { action } })
		);
	}

	async kubePlay(request: KubePlayRequest, environmentId: string): Promise<KubePlayReport> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/podman/kube/play`, request));
	}

	async kubeDown(request: KubeDownRequest, environmentId: string): Promise<KubeDownReport> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/podman/kube/down`, request));
	}

	async kubeGenerate(request: KubeGenerateRequest, environmentId: string): Promise<KubeGenerateResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/podman/kube/generate`, request));
	}

	async getSecrets(
		scope: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<Paginated<PodmanSecret>> {
		const res = await this.api.get(`/environments/${scope}/podman/secrets`, {
			...config,
			params: transformPaginationParams(options)
		});
		return res.data;
	}

	async getSecret(name: string, environmentId: string): Promise<PodmanSecret> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/podman/secrets/${encodeURIComponent(name)}`));
	}

	async createSecret(request: PodmanSecretCreateRequest, environmentId: string): Promise<PodmanSecretCreateResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/podman/secrets`, request));
	}

	async updateSecret(name: string, request: PodmanSecretUpdateRequest, environmentId: string): Promise<PodmanSecret> {
		return this.handleResponse(
			this.api.put(`/environments/${environmentId}/podman/secrets/${encodeURIComponent(name)}`, request)
		);
	}

	async removeSecret(secretId: string, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/environments/${environmentId}/podman/secrets/${encodeURIComponent(secretId)}`));
	}
}

export const podmanService = new PodmanService();
