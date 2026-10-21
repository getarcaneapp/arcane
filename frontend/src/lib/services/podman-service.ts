import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type {
	PodmanAutoUpdateOptions,
	PodmanAutoUpdateResult,
	PodmanInfo,
	PodmanKubeDownReport,
	PodmanKubeDownRequest,
	PodmanKubeGenerateRequest,
	PodmanKubeGenerateResponse,
	PodmanKubePlayReport,
	PodmanKubePlayRequest,
	PodmanPod,
	PodmanPodActionRequest,
	PodmanPodCreateRequest,
	PodmanPodCreateResponse,
	PodmanPodInspect,
	PodmanPodPruneReport,
	PodmanPodStats,
	PodmanPodTop,
	PodmanQuadletDetail,
	PodmanQuadletFile,
	PodmanQuadletInstallReport,
	PodmanQuadletInstallRequest,
	PodmanQuadletRemoveReport,
	PodmanQuadletUpdateRequest,
	PodmanSecret,
	PodmanSecretCreateRequest,
	PodmanSecretCreateResponse,
	PodmanSecretUpdateRequest,
	PodmanStatus,
	PodmanUnitStatus
} from '#lib/types/podman.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

export type PodmanPodAction = 'start' | 'stop' | 'restart' | 'pause' | 'unpause' | 'kill';
export type PodmanQuadletAction = 'start' | 'stop' | 'restart';

class PodmanService extends BaseAPIService {
	private async environmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getStatus(environmentId?: string, config?: APIRequestConfig): Promise<PodmanStatus> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/podman/status`, config));
	}

	async getInfo(environmentId?: string, config?: APIRequestConfig): Promise<PodmanInfo> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/podman/info`, config));
	}

	async runAutoUpdate(options: PodmanAutoUpdateOptions = {}, environmentId?: string): Promise<PodmanAutoUpdateResult> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/podman/autoupdate`, options));
	}

	async getPods(scope: string, options?: SearchPaginationSortRequest, config?: APIRequestConfig): Promise<Paginated<PodmanPod>> {
		const res = await this.api.get(`/environments/${scope}/podman/pods`, {
			...config,
			params: transformPaginationParams(options)
		});
		return res.data;
	}

	async getPod(podId: string, environmentId?: string): Promise<PodmanPodInspect> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/podman/pods/${encodeURIComponent(podId)}`));
	}

	async getPodStats(names: string[], environmentId?: string): Promise<PodmanPodStats[]> {
		const envId = await this.environmentId(environmentId);
		const params = names.length > 0 ? { names: names.join(',') } : undefined;
		return this.handleResponse(this.api.get(`/environments/${envId}/podman/pods/stats`, { params }));
	}

	async getPodTop(podId: string, environmentId?: string): Promise<PodmanPodTop> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/podman/pods/${encodeURIComponent(podId)}/top`));
	}

	async createPod(request: PodmanPodCreateRequest, environmentId?: string): Promise<PodmanPodCreateResponse> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/podman/pods`, request));
	}

	async prunePods(environmentId?: string): Promise<PodmanPodPruneReport[]> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/podman/pods/prune`, {}));
	}

	async podAction(
		podId: string,
		action: PodmanPodAction,
		environmentId?: string,
		request: PodmanPodActionRequest = {}
	): Promise<void> {
		const envId = await this.environmentId(environmentId);
		await this.handleResponse(
			this.api.post(`/environments/${envId}/podman/pods/${encodeURIComponent(podId)}`, request, { params: { action } })
		);
	}

	async removePod(podId: string, force = false, environmentId?: string): Promise<void> {
		const envId = await this.environmentId(environmentId);
		await this.handleResponse(
			this.api.delete(`/environments/${envId}/podman/pods/${encodeURIComponent(podId)}`, { params: { force } })
		);
	}

	async getQuadlets(
		scope: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<Paginated<PodmanQuadletDetail>> {
		const res = await this.api.get(`/environments/${scope}/podman/quadlets`, {
			...config,
			params: transformPaginationParams(options)
		});
		return res.data;
	}

	async getQuadlet(name: string, environmentId?: string): Promise<PodmanQuadletDetail> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/podman/quadlets/${encodeURIComponent(name)}`));
	}

	async getQuadletFile(name: string, environmentId?: string): Promise<PodmanQuadletFile> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/podman/quadlets/${encodeURIComponent(name)}/file`));
	}

	async installQuadlet(request: PodmanQuadletInstallRequest, environmentId?: string): Promise<PodmanQuadletInstallReport> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/podman/quadlets`, request));
	}

	async updateQuadlet(
		name: string,
		request: PodmanQuadletUpdateRequest,
		environmentId?: string
	): Promise<PodmanQuadletInstallReport> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.put(`/environments/${envId}/podman/quadlets/${encodeURIComponent(name)}`, request));
	}

	async removeQuadlet(name: string, environmentId?: string): Promise<PodmanQuadletRemoveReport> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.delete(`/environments/${envId}/podman/quadlets/${encodeURIComponent(name)}`));
	}

	async quadletAction(name: string, action: PodmanQuadletAction, environmentId?: string): Promise<PodmanUnitStatus> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/podman/quadlets/${encodeURIComponent(name)}`, {}, { params: { action } })
		);
	}

	async kubePlay(request: PodmanKubePlayRequest, environmentId?: string): Promise<PodmanKubePlayReport> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/podman/kube/play`, request));
	}

	async kubeDown(request: PodmanKubeDownRequest, environmentId?: string): Promise<PodmanKubeDownReport> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/podman/kube/down`, request));
	}

	async kubeGenerate(request: PodmanKubeGenerateRequest, environmentId?: string): Promise<PodmanKubeGenerateResponse> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/podman/kube/generate`, request));
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

	async getSecret(name: string, environmentId?: string): Promise<PodmanSecret> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/podman/secrets/${encodeURIComponent(name)}`));
	}

	async createSecret(request: PodmanSecretCreateRequest, environmentId?: string): Promise<PodmanSecretCreateResponse> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/podman/secrets`, request));
	}

	async updateSecret(name: string, request: PodmanSecretUpdateRequest, environmentId?: string): Promise<PodmanSecret> {
		const envId = await this.environmentId(environmentId);
		return this.handleResponse(this.api.put(`/environments/${envId}/podman/secrets/${encodeURIComponent(name)}`, request));
	}

	async removeSecret(secretId: string, environmentId?: string): Promise<void> {
		const envId = await this.environmentId(environmentId);
		await this.handleResponse(this.api.delete(`/environments/${envId}/podman/secrets/${encodeURIComponent(secretId)}`));
	}
}

export const podmanService = new PodmanService();
