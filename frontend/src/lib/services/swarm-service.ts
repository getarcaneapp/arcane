import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';
import type {
	SwarmServiceSummary,
	SwarmNodeSummary,
	SwarmTaskSummary,
	SwarmStackSummary,
	SwarmInfo,
	SwarmServiceCreateRequest,
	SwarmServiceUpdateRequest,
	SwarmServiceCreateResponse,
	SwarmServiceUpdateResponse,
	SwarmServiceInspect,
	SwarmStackDeployRequest,
	SwarmStackDeployResponse
} from '$lib/types/swarm.type';

export type SwarmServicesPaginatedResponse = Paginated<SwarmServiceSummary>;
export type SwarmNodesPaginatedResponse = Paginated<SwarmNodeSummary>;
export type SwarmTasksPaginatedResponse = Paginated<SwarmTaskSummary>;
export type SwarmStacksPaginatedResponse = Paginated<SwarmStackSummary>;

export class SwarmService extends BaseAPIService {
	async getServices(options?: SearchPaginationSortRequest): Promise<SwarmServicesPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/swarm/services`, { params });
		return res.data;
	}

	async getService(serviceId: string): Promise<SwarmServiceInspect> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/services/${serviceId}`));
	}

	async createService(request: SwarmServiceCreateRequest): Promise<SwarmServiceCreateResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/swarm/services`, request));
	}

	async updateService(serviceId: string, request: SwarmServiceUpdateRequest): Promise<SwarmServiceUpdateResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.put(`/environments/${envId}/swarm/services/${serviceId}`, request));
	}

	async removeService(serviceId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.delete(`/environments/${envId}/swarm/services/${serviceId}`));
	}

	async getNodes(options?: SearchPaginationSortRequest): Promise<SwarmNodesPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/swarm/nodes`, { params });
		return res.data;
	}

	async getNode(nodeId: string): Promise<SwarmNodeSummary> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/nodes/${nodeId}`));
	}

	async getTasks(options?: SearchPaginationSortRequest): Promise<SwarmTasksPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/swarm/tasks`, { params });
		return res.data;
	}

	async getStacks(options?: SearchPaginationSortRequest): Promise<SwarmStacksPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/swarm/stacks`, { params });
		return res.data;
	}

	async deployStack(request: SwarmStackDeployRequest): Promise<SwarmStackDeployResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/swarm/stacks`, request));
	}

	async getSwarmInfo(): Promise<SwarmInfo> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/info`));
	}
}

export const swarmService = new SwarmService();
