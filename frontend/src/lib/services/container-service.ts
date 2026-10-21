import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { Activity } from '#lib/types/activity.type.js';
import type {
	ContainerStatusCounts,
	ContainerSummaryDto,
	ContainerSummaryGroupDto,
	ContainerCreate,
	ContainerDetailsDto,
	ContainerCommitRequest,
	ContainerCommitResult,
	ContainerEditConfigDto,
	ContainerEditRequest,
	ContainerProcessesDto
} from '#lib/types/docker.js';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import { downloadFromUrl } from '#lib/utils/browser-download.js';
import { transformPaginationParams } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import BaseAPIService, {
	type APIRequestConfig,
	extractServerMessage,
	handleUnauthorizedResponseInternal,
	withSessionSignal
} from './api-service';

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
		options?: ContainerListRequestOptions,
		config?: APIRequestConfig
	): Promise<ContainersPaginatedResponse> {
		const params = transformPaginationParams(options);
		if (options?.groupByProject) {
			params['groupBy'] = 'project';
		}
		const res = await this.api.get(`/environments/${environmentId}/containers`, { ...config, params });
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

	async getContainerProcesses(containerId: string, signal?: AbortSignal, environmentId?: string): Promise<ContainerProcessesDto> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/containers/${containerId}/processes`, { signal }));
	}

	async startContainer(containerId: string, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/containers/${containerId}`, undefined, { params: { action: 'start' } })
		);
	}

	async createContainer(options: ContainerCreate, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/containers`, options));
	}

	async stopContainer(containerId: string, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/containers/${containerId}`, undefined, { params: { action: 'stop' } })
		);
	}

	async restartContainer(containerId: string, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/containers/${containerId}`, undefined, { params: { action: 'restart' } })
		);
	}

	async killContainer(containerId: string, signal?: string, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const params: Record<string, string> = { action: 'kill' };
		if (signal) params['signal'] = signal;
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}`, undefined, { params }));
	}

	async pauseContainer(containerId: string, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/containers/${containerId}`, undefined, { params: { action: 'pause' } })
		);
	}

	async unpauseContainer(containerId: string, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/containers/${containerId}`, undefined, { params: { action: 'unpause' } })
		);
	}

	async commitContainer(
		containerId: string,
		request: ContainerCommitRequest,
		environmentId?: string
	): Promise<ContainerCommitResult> {
		const envId = await this.resolveEnvironmentId(environmentId);
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

	updateContainer(containerId: string, environmentId: string): Promise<Activity> {
		return this.handleResponse<Activity>(
			this.api.post(`/environments/${environmentId}/containers/${containerId}/update`, undefined, {
				params: { async: 'true' }
			})
		).then((activity) => ({ ...activity, sourceEnvironmentId: environmentId }));
	}

	async redeployContainer(containerId: string, environmentId?: string): Promise<ContainerDetailsDto> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/redeploy`));
	}

	async generateCompose(containerIds: string[], environmentId?: string): Promise<{ composeContent: string }> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/compose`, { containerIds }));
	}

	async getContainerEditConfig(containerId: string, environmentId?: string): Promise<ContainerEditConfigDto> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/containers/${containerId}/config`));
	}

	async editContainer(containerId: string, request: ContainerEditRequest, environmentId?: string): Promise<ContainerDetailsDto> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.put(`/environments/${envId}/containers/${containerId}/config`, request));
	}

	async setAutoUpdate(
		containerId: string,
		enabled: boolean,
		environmentId?: string
	): Promise<{ success: boolean; data: { message: string } }> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.put(`/environments/${envId}/containers/${containerId}/autoupdate`, { enabled }));
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
		const response = await fetch(url, { credentials: 'include', signal: withSessionSignal(probe.signal) });
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
