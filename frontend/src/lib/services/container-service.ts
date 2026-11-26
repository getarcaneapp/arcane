import type { Activity } from '#lib/types/activity.js';
import type { MessageResponse } from '#lib/types/base.js';
import type {
	ContainerPaginatedResponse,
	ContainerStatusCounts,
	ContainerCreated,
	ContainerDetails,
	ContainerCommitRequest,
	ContainerCommitResult,
	ContainerEditConfig,
	ContainerEdit,
	ContainerProcesses
} from '#lib/types/container.js';
import type { ContainerCreate, ContainerListRequestOptions } from '#lib/types/ui/container.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

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
		const res = await this.api.get<ContainerPaginatedResponse>(`/agents/${environmentId}/containers`, { ...config, params });
		return res.data;
	}

	async getContainerStatusCountsForEnvironment(environmentId: string, config?: APIRequestConfig): Promise<ContainerStatusCounts> {
		const res = await this.api.get<{ data: ContainerStatusCounts }>(`/agents/${environmentId}/containers/counts`, config);
		return res.data.data;
	}

	async getContainerForEnvironment(environmentId: string, containerId: string): Promise<ContainerDetails> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/containers/${containerId}`));
	}

	async getSwarmTaskHostnames(
		environmentId: string,
		containerIds: string[],
		signal: AbortSignal
	): Promise<Record<string, string>> {
		return this.handleResponse(
			this.api.post(
				`/agents/${environmentId}/containers/swarm-tasks/hostnames`,
				{ containerIds },
				{ signal, suppressAccessDeniedToast: true }
			)
		);
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
		await this.downloadFile(`/agents/${environmentId}/containers/${containerId}/logs/download`);
	}
}

export const containerService = new ContainerService();
