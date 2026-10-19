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
	PodmanStatus,
	PodmanUnitStatus
} from '#lib/types/podman.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

export type PodmanPodAction = 'start' | 'stop' | 'restart' | 'pause' | 'unpause' | 'kill';
export type PodmanQuadletAction = 'start' | 'stop' | 'restart';

class PodmanService extends BaseAPIService {
	private async basePath(): Promise<string> {
		return `/environments/${await environmentStore.getCurrentEnvironmentId()}/podman`;
	}

	async getStatus(environmentId?: string, config?: APIRequestConfig): Promise<PodmanStatus> {
		const envId = environmentId ?? (await environmentStore.getCurrentEnvironmentId());
		return this.handleResponse(this.api.get(`/environments/${envId}/podman/status`, config));
	}

	async getInfo(): Promise<PodmanInfo> {
		return this.handleResponse(this.api.get(`${await this.basePath()}/info`));
	}

	async runAutoUpdate(options: PodmanAutoUpdateOptions = {}): Promise<PodmanAutoUpdateResult> {
		return this.handleResponse(this.api.post(`${await this.basePath()}/autoupdate`, options));
	}

	async getPods(): Promise<PodmanPod[]> {
		return this.handleResponse(this.api.get(`${await this.basePath()}/pods`));
	}

	async getPod(podId: string): Promise<PodmanPodInspect> {
		return this.handleResponse(this.api.get(`${await this.basePath()}/pods/${encodeURIComponent(podId)}`));
	}

	async getPodStats(names: string[] = []): Promise<PodmanPodStats[]> {
		const params = names.length > 0 ? { names: names.join(',') } : undefined;
		return this.handleResponse(this.api.get(`${await this.basePath()}/pods/stats`, { params }));
	}

	async getPodTop(podId: string): Promise<PodmanPodTop> {
		return this.handleResponse(this.api.get(`${await this.basePath()}/pods/${encodeURIComponent(podId)}/top`));
	}

	async createPod(request: PodmanPodCreateRequest): Promise<PodmanPodCreateResponse> {
		return this.handleResponse(this.api.post(`${await this.basePath()}/pods`, request));
	}

	async prunePods(): Promise<PodmanPodPruneReport[]> {
		return this.handleResponse(this.api.post(`${await this.basePath()}/pods/prune`, {}));
	}

	async podAction(podId: string, action: PodmanPodAction, request: PodmanPodActionRequest = {}): Promise<void> {
		await this.handleResponse(this.api.post(`${await this.basePath()}/pods/${encodeURIComponent(podId)}/${action}`, request));
	}

	async removePod(podId: string, force = false): Promise<void> {
		await this.handleResponse(
			this.api.delete(`${await this.basePath()}/pods/${encodeURIComponent(podId)}`, { params: { force } })
		);
	}

	async getQuadlets(): Promise<PodmanQuadletDetail[]> {
		return (await this.handleResponse<PodmanQuadletDetail[] | null>(this.api.get(`${await this.basePath()}/quadlets`))) ?? [];
	}

	async getQuadlet(name: string): Promise<PodmanQuadletDetail> {
		return this.handleResponse(this.api.get(`${await this.basePath()}/quadlets/${encodeURIComponent(name)}`));
	}

	async getQuadletFile(name: string): Promise<PodmanQuadletFile> {
		return this.handleResponse(this.api.get(`${await this.basePath()}/quadlets/${encodeURIComponent(name)}/file`));
	}

	async installQuadlet(request: PodmanQuadletInstallRequest): Promise<PodmanQuadletInstallReport> {
		return this.handleResponse(this.api.post(`${await this.basePath()}/quadlets`, request));
	}

	async updateQuadlet(name: string, request: PodmanQuadletUpdateRequest): Promise<PodmanQuadletInstallReport> {
		return this.handleResponse(this.api.put(`${await this.basePath()}/quadlets/${encodeURIComponent(name)}`, request));
	}

	async removeQuadlet(name: string): Promise<PodmanQuadletRemoveReport> {
		return this.handleResponse(this.api.delete(`${await this.basePath()}/quadlets/${encodeURIComponent(name)}`));
	}

	async quadletAction(name: string, action: PodmanQuadletAction): Promise<PodmanUnitStatus> {
		return this.handleResponse(this.api.post(`${await this.basePath()}/quadlets/${encodeURIComponent(name)}/${action}`, {}));
	}

	async kubePlay(request: PodmanKubePlayRequest): Promise<PodmanKubePlayReport> {
		return this.handleResponse(this.api.post(`${await this.basePath()}/kube/play`, request));
	}

	async kubeDown(request: PodmanKubeDownRequest): Promise<PodmanKubeDownReport> {
		return this.handleResponse(this.api.post(`${await this.basePath()}/kube/down`, request));
	}

	async kubeGenerate(request: PodmanKubeGenerateRequest): Promise<PodmanKubeGenerateResponse> {
		return this.handleResponse(this.api.post(`${await this.basePath()}/kube/generate`, request));
	}

	async getSecrets(): Promise<PodmanSecret[]> {
		return this.handleResponse(this.api.get(`${await this.basePath()}/secrets`));
	}

	async createSecret(request: PodmanSecretCreateRequest): Promise<PodmanSecretCreateResponse> {
		return this.handleResponse(this.api.post(`${await this.basePath()}/secrets`, request));
	}

	async removeSecret(secretId: string): Promise<void> {
		await this.handleResponse(this.api.delete(`${await this.basePath()}/secrets/${encodeURIComponent(secretId)}`));
	}
}

export const podmanService = new PodmanService();
