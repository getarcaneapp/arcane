import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type {
	ContainerStatusCounts,
	ContainerSummaryDto,
	ContainerStats,
	ContainerCreateRequest
} from '$lib/types/container.type';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export type ContainersPaginatedResponse = Paginated<ContainerSummaryDto, ContainerStatusCounts>;

export class ContainerService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getContainers(options?: SearchPaginationSortRequest): Promise<ContainersPaginatedResponse> {
		const envId = await this.resolveEnvironmentId();
		return this.getContainersForEnvironment(envId, options);
	}

	async getContainersForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest
	): Promise<ContainersPaginatedResponse> {
		const params = transformPaginationParams(options);
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

	async getContainer(containerId: string): Promise<any> {
		const envId = await this.resolveEnvironmentId();
		return this.getContainerForEnvironment(envId, containerId);
	}

	async getContainerForEnvironment(environmentId: string, containerId: string): Promise<any> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/containers/${containerId}`));
	}

	async startContainer(containerId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/start`));
	}

	async createContainer(options: ContainerCreateRequest): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
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

	async deleteContainer(containerId: string, opts?: { force?: boolean; volumes?: boolean }): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params: Record<string, string> = {};
		if (opts?.force !== undefined) params.force = String(!!opts.force);
		if (opts?.volumes !== undefined) params.volumes = String(!!opts.volumes);

		return this.handleResponse(this.api.delete(`/environments/${envId}/containers/${containerId}`, { params }));
	}

	async updateContainer(containerId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/containers/${containerId}/update`));
	}
}

export const containerService = new ContainerService();
