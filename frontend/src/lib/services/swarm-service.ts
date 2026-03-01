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
	SwarmStackDeployResponse,
	SwarmServiceScaleRequest,
	SwarmNodeUpdateRequest,
	SwarmStackInspect,
	SwarmStackRenderConfigRequest,
	SwarmStackRenderConfigResponse,
	SwarmStackSource,
	SwarmInitRequest,
	SwarmInitResponse,
	SwarmJoinRequest,
	SwarmLeaveRequest,
	SwarmUnlockRequest,
	SwarmUnlockKeyResponse,
	SwarmJoinTokensResponse,
	SwarmRotateJoinTokensRequest,
	SwarmUpdateRequest,
	SwarmConfigSummary,
	SwarmSecretSummary,
	SwarmConfigCreateRequest,
	SwarmConfigUpdateRequest,
	SwarmSecretCreateRequest,
	SwarmSecretUpdateRequest
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

	async getServiceTasks(serviceId: string, options?: SearchPaginationSortRequest): Promise<SwarmTasksPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/swarm/services/${serviceId}/tasks`, { params });
		return res.data;
	}

	async rollbackService(serviceId: string): Promise<SwarmServiceUpdateResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/swarm/services/${serviceId}/rollback`, {}));
	}

	async scaleService(serviceId: string, request: SwarmServiceScaleRequest): Promise<SwarmServiceUpdateResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/swarm/services/${serviceId}/scale`, request));
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

	async updateNode(nodeId: string, request: SwarmNodeUpdateRequest): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.patch(`/environments/${envId}/swarm/nodes/${nodeId}`, request));
	}

	async removeNode(nodeId: string, force = false): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.delete(`/environments/${envId}/swarm/nodes/${nodeId}`, { params: { force } }));
	}

	async promoteNode(nodeId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/swarm/nodes/${nodeId}/promote`, {}));
	}

	async demoteNode(nodeId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/swarm/nodes/${nodeId}/demote`, {}));
	}

	async getNodeTasks(nodeId: string, options?: SearchPaginationSortRequest): Promise<SwarmTasksPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/swarm/nodes/${nodeId}/tasks`, { params });
		return res.data;
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

	async getStack(name: string): Promise<SwarmStackInspect> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/stacks/${name}`));
	}

	async getStackSource(name: string): Promise<SwarmStackSource> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/stacks/${name}/source`));
	}

	async removeStack(name: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.delete(`/environments/${envId}/swarm/stacks/${name}`));
	}

	async getStackServices(name: string, options?: SearchPaginationSortRequest): Promise<SwarmServicesPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/swarm/stacks/${name}/services`, { params });
		return res.data;
	}

	async getStackTasks(name: string, options?: SearchPaginationSortRequest): Promise<SwarmTasksPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/swarm/stacks/${name}/tasks`, { params });
		return res.data;
	}

	async renderStackConfig(request: SwarmStackRenderConfigRequest): Promise<SwarmStackRenderConfigResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/swarm/stacks/config/render`, request));
	}

	async getSwarmInfo(): Promise<SwarmInfo> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/info`));
	}

	async initSwarm(request: SwarmInitRequest): Promise<SwarmInitResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/swarm/init`, request));
	}

	async joinSwarm(request: SwarmJoinRequest): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/swarm/join`, request));
	}

	async leaveSwarm(request: SwarmLeaveRequest): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/swarm/leave`, request));
	}

	async unlockSwarm(request: SwarmUnlockRequest): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/swarm/unlock`, request));
	}

	async getSwarmUnlockKey(): Promise<SwarmUnlockKeyResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/unlock-key`));
	}

	async getSwarmJoinTokens(): Promise<SwarmJoinTokensResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/join-tokens`));
	}

	async rotateSwarmJoinTokens(request: SwarmRotateJoinTokensRequest = {}): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/swarm/join-tokens/rotate`, request));
	}

	async updateSwarmSpec(request: SwarmUpdateRequest): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.put(`/environments/${envId}/swarm/spec`, request));
	}

	async getConfigs(): Promise<SwarmConfigSummary[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/configs`));
	}

	async getConfig(configId: string): Promise<SwarmConfigSummary> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/configs/${configId}`));
	}

	async createConfig(request: SwarmConfigCreateRequest): Promise<SwarmConfigSummary> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/swarm/configs`, request));
	}

	async updateConfig(configId: string, request: SwarmConfigUpdateRequest): Promise<SwarmConfigSummary> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.put(`/environments/${envId}/swarm/configs/${configId}`, request));
	}

	async removeConfig(configId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.delete(`/environments/${envId}/swarm/configs/${configId}`));
	}

	async getSecrets(): Promise<SwarmSecretSummary[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/secrets`));
	}

	async getSecret(secretId: string): Promise<SwarmSecretSummary> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/swarm/secrets/${secretId}`));
	}

	async createSecret(request: SwarmSecretCreateRequest): Promise<SwarmSecretSummary> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/swarm/secrets`, request));
	}

	async updateSecret(secretId: string, request: SwarmSecretUpdateRequest): Promise<SwarmSecretSummary> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.put(`/environments/${envId}/swarm/secrets/${secretId}`, request));
	}

	async removeSecret(secretId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.delete(`/environments/${envId}/swarm/secrets/${secretId}`));
	}
}

export const swarmService = new SwarmService();
