import type { Activity } from '#lib/types/activity.js';
import type { MessageResponse } from '#lib/types/base.js';
import type {
	ContainerPaginatedResponse,
	ContainerCreated,
	ContainerDetails,
	ContainerCommitRequest,
	ContainerCommitResult,
	ContainerEditConfig,
	ContainerEdit,
	ContainerProcesses
} from '#lib/types/container.js';
import type { ContainerCreate, ContainerListRequestOptions } from '#lib/types/ui/container.js';
import { downloadFromUrl } from '#lib/utils/browser-download.js';
import { transformPaginationParams } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import BaseAPIService, {
	type APIRequestConfig,
	extractServerMessage,
	handleUnauthorizedResponseInternal,
	withSessionSignal
} from './api-service';

class ContainerService extends BaseAPIService {
	async getContainersForEnvironment(
		environmentId: string,
		options?: ContainerListRequestOptions,
		config?: APIRequestConfig
	): Promise<ContainerPaginatedResponse> {
		const params = transformPaginationParams(options);
		if (options?.groupByProject) {
			params['groupBy'] = 'project';
		}
		const res = await this.api.get(`/agents/${environmentId}/containers`, { ...config, params });
		return res.data;
	}

	async getContainerForEnvironment(environmentId: string, containerId: string): Promise<ContainerDetails> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/containers/${containerId}`));
	}

	async getContainerProcesses(
		containerId: string,
		signal: AbortSignal | undefined,
		environmentId: string
	): Promise<ContainerProcesses> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/containers/${containerId}/processes`, { signal }));
	}

	async startContainer(containerId: string, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/containers/${containerId}`, undefined, { params: { action: 'start' } })
		);
	}

	async createContainer(options: ContainerCreate, environmentId: string): Promise<ContainerCreated> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/containers`, options));
	}

	async stopContainer(containerId: string, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/containers/${containerId}`, undefined, { params: { action: 'stop' } })
		);
	}

	async restartContainer(containerId: string, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/containers/${containerId}`, undefined, { params: { action: 'restart' } })
		);
	}

	async killContainer(containerId: string, signal: string | undefined, environmentId: string): Promise<MessageResponse> {
		const params: Record<string, string> = { action: 'kill' };
		if (signal) params['signal'] = signal;
		return this.handleResponse(this.api.post(`/agents/${environmentId}/containers/${containerId}`, undefined, { params }));
	}

	async pauseContainer(containerId: string, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/containers/${containerId}`, undefined, { params: { action: 'pause' } })
		);
	}

	async unpauseContainer(containerId: string, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/containers/${containerId}`, undefined, { params: { action: 'unpause' } })
		);
	}

	async commitContainer(
		containerId: string,
		request: ContainerCommitRequest,
		environmentId: string
	): Promise<ContainerCommitResult> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/containers/${containerId}/commit`, request));
	}

	async deleteContainer(
		containerId: string,
		opts: { force?: boolean; volumes?: boolean; environmentId: string }
	): Promise<MessageResponse> {
		const params: Record<string, string> = {};
		if (opts.force !== undefined) params['force'] = String(!!opts.force);
		if (opts.volumes !== undefined) params['volumes'] = String(!!opts.volumes);

		return this.handleResponse(this.api.delete(`/agents/${opts.environmentId}/containers/${containerId}`, { params }));
	}

	updateContainer(containerId: string, environmentId: string): Promise<Activity> {
		return this.handleResponse<Activity>(
			this.api.post(`/agents/${environmentId}/containers/${containerId}/update`, undefined, {
				params: { async: 'true' }
			})
		).then((activity) => ({ ...activity, sourceEnvironmentId: environmentId }));
	}

	async redeployContainer(containerId: string, environmentId: string): Promise<ContainerDetails> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/containers/${containerId}/redeploy`));
	}

	async generateCompose(containerIds: string[], environmentId: string): Promise<{ composeContent: string }> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/containers/compose`, { containerIds }));
	}

	async getContainerEditConfig(containerId: string, environmentId: string): Promise<ContainerEditConfig> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/containers/${containerId}/config`));
	}

	async editContainer(containerId: string, request: ContainerEdit, environmentId: string): Promise<ContainerDetails> {
		return this.handleResponse(this.api.put(`/agents/${environmentId}/containers/${containerId}/config`, request));
	}

	async setAutoUpdate(
		containerId: string,
		enabled: boolean,
		environmentId: string
	): Promise<{ success: boolean; data: { message: string } }> {
		return this.handleResponse(this.api.put(`/agents/${environmentId}/containers/${containerId}/autoupdate`, { enabled }));
	}

	async downloadContainerLogs(containerId: string, environmentId: string): Promise<void> {
		const base = this.api.defaults.baseURL;
		const url = `${base}/agents/${environmentId}/containers/${containerId}/logs/download`;
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
