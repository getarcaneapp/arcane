import BaseAPIService, { extractServerMessage, handleUnauthorizedResponseInternal } from './api-service';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type {
	ContainerStatusCounts,
	ContainerSummaryDto,
	ContainerSummaryGroupDto,
	ContainerCreateRequest,
	ContainerDetailsDto,
	ContainerCommitRequest,
	ContainerCommitResult,
	ContainerEditConfigDto,
	ContainerEditRequest
} from '#lib/types/docker.js';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import type { AutoUpdateResult } from '#lib/types/automation.js';
import { transformPaginationParams } from '#lib/utils/tables.js';
import { downloadFromUrl } from '#lib/utils/browser-download.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export type ContainersPaginatedResponse = Paginated<ContainerSummaryDto, ContainerStatusCounts> & {
	groups?: ContainerSummaryGroupDto[];
	resourceSortSupported?: boolean;
};
export type ContainerListRequestOptions = SearchPaginationSortRequest & {
	groupByProject?: boolean;
};

class ContainerService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getContainers(options?: ContainerListRequestOptions): Promise<ContainersPaginatedResponse> {
		const envId = await this.resolveEnvironmentId();
		return this.getContainersForEnvironment(envId, options);
	}

	async getContainersForEnvironment(
		environmentId: string,
		options?: ContainerListRequestOptions
	): Promise<ContainersPaginatedResponse> {
		const params = transformPaginationParams(options);
		if (options?.groupByProject) {
			params['groupBy'] = 'project';
		}
		const res = await this.api.get(`/environments/${environmentId}/containers`, { params });
		return res.data;
	}

	async getContainerStatusCounts(): Promise<ContainerStatusCounts> {
		const envId = await this.resolveEnvironmentId();
		return this.getContainerStatusCountsForEnvironment(envId);
	}

	async getContainerStatusCountsForEnvironment(environmentId: string): Promise<ContainerStatusCounts> {
		const res = await this.api.get(`/environments/${environmentId}/containers/counts`);
		return res.data.data;
	}

	async getContainer(containerId: string): Promise<ContainerDetailsDto> {
		const envId = await this.resolveEnvironmentId();
		return this.getContainerForEnvironment(envId, containerId);
	}

	async getContainerForEnvironment(environmentId: string, containerId: string): Promise<ContainerDetailsDto> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/containers/${containerId}`));
	}

	async startContainer(containerId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/start`));
	}

	async createContainer(options: ContainerCreateRequest, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/containers`, options));
	}

	async stopContainer(containerId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/stop`));
	}

	async restartContainer(containerId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/restart`));
	}

	async killContainer(containerId: string, signal?: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params: Record<string, string> = {};
		if (signal) params['signal'] = signal;
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/kill`, undefined, { params }));
	}

	async pauseContainer(containerId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/pause`));
	}

	async unpauseContainer(containerId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/unpause`));
	}

	async commitContainer(containerId: string, request: ContainerCommitRequest): Promise<ContainerCommitResult> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/commit`, request));
	}

	async deleteContainer(
		containerId: string,
		opts?: { force?: boolean; volumes?: boolean; environmentId?: string }
	): Promise<any> {
		const envId = await this.resolveEnvironmentId(opts?.environmentId);
		const params: Record<string, string> = {};
		if (opts?.force !== undefined) params['force'] = String(!!opts.force);
		if (opts?.volumes !== undefined) params['volumes'] = String(!!opts.volumes);

		return this.handleResponse(this.api.delete(`/environments/${envId}/containers/${containerId}`, { params }));
	}

	async updateContainer(containerId: string): Promise<AutoUpdateResult> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/update`));
	}

	async redeployContainer(containerId: string): Promise<ContainerDetailsDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/redeploy`));
	}

	async generateCompose(containerIds: string[], environmentId?: string): Promise<{ composeContent: string }> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/generate-compose`, { containerIds }));
	}

	async getContainerEditConfig(containerId: string, environmentId?: string): Promise<ContainerEditConfigDto> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/containers/${containerId}/edit-config`));
	}

	async editContainer(containerId: string, request: ContainerEditRequest): Promise<ContainerDetailsDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/edit`, request));
	}

	async setAutoUpdate(containerId: string, enabled: boolean): Promise<{ success: boolean; data: { message: string } }> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.put(`/environments/${envId}/containers/${containerId}/auto-update`, { enabled }));
	}

	async downloadContainerLogs(containerId: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const base = this.api.defaults.baseURL.replace(/\/+$/, '');
		const url = `${base}/environments/${envId}/containers/${containerId}/logs/download`;
		if (await this.probeDownloadInternal(url)) {
			downloadFromUrl(url);
		}
	}

	private async probeDownloadInternal(url: string, retry = false): Promise<boolean> {
		const probe = new AbortController();
		const response = await fetch(url, { credentials: 'include', signal: probe.signal });
		if (response.ok) {
			probe.abort();
			return true;
		}
		const body = await tryCatch(response.json());
		const message = extractServerMessage(body.data, true);
		if (response.status === 401 && !retry) {
			const path = new URL(url, window.location.origin).pathname;
			const action = await handleUnauthorizedResponseInternal(path, false, message);
			if (action === 'retry') return this.probeDownloadInternal(url, true);
			if (action !== 'none') return false;
		}
		throw new Error(message ?? `${response.status} ${response.statusText}`.trim());
	}
}

export const containerService = new ContainerService();
