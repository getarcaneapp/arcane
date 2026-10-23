import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import type {
	SwarmServiceSummary,
	SwarmNode,
	SwarmTaskSummary,
	SwarmStackSummary,
	SwarmInfo,
	SwarmRuntimeStatus,
	SwarmNodeAgentDeployment,
	SwarmNodeAgentBindingRequest,
	SwarmNodeAgentReconcileResponse,
	SwarmServiceCreateRequest,
	SwarmServiceUpdateRequest,
	SwarmServiceCreateResponse,
	SwarmServiceUpdateResponse,
	SwarmServiceInspect,
	SwarmStackDeployRequest,
	SwarmStackDeployResponse,
	SwarmServiceScaleRequest,
	SwarmNodeUpdate,
	SwarmStackInspect,
	SwarmStackRenderConfigRequest,
	SwarmStackRenderConfigResponse,
	SwarmStackSource,
	SwarmStackSourceUpdate,
	SwarmInitRequest,
	SwarmInitResponse,
	SwarmJoinRequest,
	SwarmLeaveRequest,
	SwarmUnlockRequest,
	SwarmUnlockKeyResponse,
	SwarmJoinTokensResponse,
	SwarmRotateJoinTokensRequest,
	SwarmUpdateRequest,
	SwarmKvCreateRequest,
	SwarmKvKind,
	SwarmKvSummary,
	SwarmKvUpdateRequest,
	SwarmJoinCandidate,
	SwarmJoinEnvironmentsRequest,
	SwarmJoinEnvironmentsResponse
} from '#lib/types/swarm.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

export type SwarmServicesPaginatedResponse = Paginated<SwarmServiceSummary>;
export type SwarmNodesPaginatedResponse = Paginated<SwarmNode>;
export type SwarmTasksPaginatedResponse = Paginated<SwarmTaskSummary>;
export type SwarmStacksPaginatedResponse = Paginated<SwarmStackSummary>;

class SwarmService extends BaseAPIService {
	async getServices(
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<SwarmServicesPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/swarm/services`, { ...config, params });
		return res.data;
	}

	async getService(serviceId: string, environmentId: string): Promise<SwarmServiceInspect> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/swarm/services/${serviceId}`));
	}

	async createService(request: SwarmServiceCreateRequest, environmentId: string): Promise<SwarmServiceCreateResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/services`, request));
	}

	async updateService(
		serviceId: string,
		request: SwarmServiceUpdateRequest,
		environmentId: string
	): Promise<SwarmServiceUpdateResponse> {
		return this.handleResponse(this.api.put(`/environments/${environmentId}/swarm/services/${serviceId}`, request));
	}

	async getServiceTasks(
		serviceId: string,
		options: SearchPaginationSortRequest | undefined,
		environmentId: string
	): Promise<SwarmTasksPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/swarm/services/${serviceId}/tasks`, { params });
		return res.data;
	}

	async rollbackService(serviceId: string, environmentId: string): Promise<SwarmServiceUpdateResponse> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/swarm/services/${serviceId}`, undefined, { params: { action: 'rollback' } })
		);
	}

	async scaleService(
		serviceId: string,
		request: SwarmServiceScaleRequest,
		environmentId: string
	): Promise<SwarmServiceUpdateResponse> {
		return this.handleResponse(this.api.patch(`/environments/${environmentId}/swarm/services/${serviceId}`, request));
	}

	async removeService(serviceId: string, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/environments/${environmentId}/swarm/services/${serviceId}`));
	}

	async getNodes(
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<SwarmNodesPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/swarm/nodes`, { ...config, params });
		return res.data;
	}

	async getNodeAgentDeployment(nodeId: string, rotate = false, environmentId: string): Promise<SwarmNodeAgentDeployment> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/swarm/nodes/${nodeId}/agent/deployment`, { rotate })
		);
	}

	async reconcileNodeAgents(environmentId: string): Promise<SwarmNodeAgentReconcileResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/nodes/agents/reconcile`, {}));
	}

	async bindNodeAgent(nodeId: string, request: SwarmNodeAgentBindingRequest, environmentId: string): Promise<SwarmNode> {
		return this.handleResponse(this.api.put(`/environments/${environmentId}/swarm/nodes/${nodeId}/agent/binding`, request));
	}

	async detachNodeAgent(nodeId: string, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/environments/${environmentId}/swarm/nodes/${nodeId}/agent/binding`));
	}

	async removeNodeAgentDeployment(nodeId: string, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/environments/${environmentId}/swarm/nodes/${nodeId}/agent/deployment`));
	}

	async updateNode(nodeId: string, request: SwarmNodeUpdate, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.patch(`/environments/${environmentId}/swarm/nodes/${nodeId}`, request));
	}

	async removeNode(nodeId: string, force = false, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/environments/${environmentId}/swarm/nodes/${nodeId}`, { params: { force } }));
	}

	async promoteNode(nodeId: string, environmentId: string): Promise<void> {
		await this.handleResponse(
			this.api.post(`/environments/${environmentId}/swarm/nodes/${nodeId}`, undefined, { params: { action: 'promote' } })
		);
	}

	async demoteNode(nodeId: string, environmentId: string): Promise<void> {
		await this.handleResponse(
			this.api.post(`/environments/${environmentId}/swarm/nodes/${nodeId}`, undefined, { params: { action: 'demote' } })
		);
	}

	async getNodeTasks(
		nodeId: string,
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<SwarmTasksPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/swarm/nodes/${nodeId}/tasks`, { ...config, params });
		return res.data;
	}

	async getTasks(
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<SwarmTasksPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/swarm/tasks`, { ...config, params });
		return res.data;
	}

	async getStacks(
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<SwarmStacksPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/swarm/stacks`, { ...config, params });
		return res.data;
	}

	async deployStack(request: SwarmStackDeployRequest, environmentId: string): Promise<SwarmStackDeployResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/stacks`, request));
	}

	async getStack(name: string, environmentId: string): Promise<SwarmStackInspect> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/swarm/stacks/${name}`));
	}

	async getStackSource(name: string, environmentId: string): Promise<SwarmStackSource> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/swarm/stacks/${name}/source`));
	}

	async updateStackSource(name: string, request: SwarmStackSourceUpdate, environmentId: string): Promise<SwarmStackSource> {
		return this.handleResponse(this.api.patch(`/environments/${environmentId}/swarm/stacks/${name}/source`, request));
	}

	async removeStack(name: string, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/environments/${environmentId}/swarm/stacks/${name}`));
	}

	async getStackServices(
		name: string,
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<SwarmServicesPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/swarm/stacks/${name}/services`, { ...config, params });
		return res.data;
	}

	async getStackTasks(
		name: string,
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<SwarmTasksPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/swarm/stacks/${name}/tasks`, { ...config, params });
		return res.data;
	}

	async renderStackConfig(
		request: SwarmStackRenderConfigRequest,
		environmentId: string
	): Promise<SwarmStackRenderConfigResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/stacks/config/render`, request));
	}

	async getSwarmInfo(environmentId: string): Promise<SwarmInfo> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/swarm/info`));
	}

	async getSwarmStatus(environmentId: string, config?: APIRequestConfig): Promise<SwarmRuntimeStatus> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/swarm/status`, config));
	}

	async initSwarm(request: SwarmInitRequest, environmentId: string): Promise<SwarmInitResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/init`, request));
	}

	async joinSwarm(request: SwarmJoinRequest, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/join`, request));
	}

	async getSwarmJoinCandidates(environmentId: string, config?: APIRequestConfig): Promise<SwarmJoinCandidate[]> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/swarm/join/candidates`, config));
	}

	async joinEnvironments(request: SwarmJoinEnvironmentsRequest, environmentId: string): Promise<SwarmJoinEnvironmentsResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/join/environments`, request));
	}

	async leaveSwarm(request: SwarmLeaveRequest, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/leave`, request));
	}

	async unlockSwarm(request: SwarmUnlockRequest, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/unlock`, request));
	}

	async getSwarmUnlockKey(environmentId: string): Promise<SwarmUnlockKeyResponse> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/swarm/unlock/key`));
	}

	async getSwarmJoinTokens(environmentId: string): Promise<SwarmJoinTokensResponse> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/swarm/join/tokens`));
	}

	async rotateSwarmJoinTokens(request: SwarmRotateJoinTokensRequest = {}, environmentId: string): Promise<void> {
		await this.handleResponse(
			this.api.post(`/environments/${environmentId}/swarm/join/tokens`, request, { params: { action: 'rotate' } })
		);
	}

	async updateSwarmSpec(request: SwarmUpdateRequest, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.put(`/environments/${environmentId}/swarm/spec`, request));
	}

	async getKvItems(
		kind: SwarmKvKind,
		options: SearchPaginationSortRequest | undefined,
		environmentId: string,
		config?: APIRequestConfig
	): Promise<Paginated<SwarmKvSummary>> {
		const res = await this.api.get(`/environments/${environmentId}/swarm/${kind}`, {
			...config,
			params: transformPaginationParams(options)
		});
		return res.data;
	}

	async getKvItem(kind: SwarmKvKind, id: string, environmentId: string): Promise<SwarmKvSummary> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/swarm/${kind}/${encodeURIComponent(id)}`));
	}

	async createKvItem(kind: SwarmKvKind, request: SwarmKvCreateRequest, environmentId: string): Promise<SwarmKvSummary> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/swarm/${kind}`, request));
	}

	async updateKvItem(
		kind: SwarmKvKind,
		id: string,
		request: SwarmKvUpdateRequest,
		environmentId: string
	): Promise<SwarmKvSummary> {
		return this.handleResponse(this.api.put(`/environments/${environmentId}/swarm/${kind}/${encodeURIComponent(id)}`, request));
	}

	async removeKvItem(kind: SwarmKvKind, id: string, environmentId: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/environments/${environmentId}/swarm/${kind}/${encodeURIComponent(id)}`));
	}
}

export const swarmService = new SwarmService();
