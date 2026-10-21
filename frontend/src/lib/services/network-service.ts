import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type {
	NetworkSummaryDto,
	NetworkUsageCounts,
	NetworkCreate,
	NetworkCreateOptions,
	NetworkInspectDto,
	NetworkTopologyDto
} from '#lib/types/docker.js';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

export type NetworksPaginatedResponse = Paginated<NetworkSummaryDto, NetworkUsageCounts>;

class NetworkService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getNetworks(options?: SearchPaginationSortRequest): Promise<NetworksPaginatedResponse> {
		const envId = await this.resolveEnvironmentId();
		return this.getNetworksForEnvironment(envId, options);
	}

	async getNetworksForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<NetworksPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/networks`, { ...config, params });
		return res.data;
	}

	async getNetworkUsageCounts(): Promise<NetworkUsageCounts> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/networks/counts`);
		return res.data.data;
	}

	async getNetwork(networkId: string, options?: SearchPaginationSortRequest): Promise<NetworkInspectDto> {
		const envId = await this.resolveEnvironmentId();
		return this.getNetworkForEnvironment(envId, networkId, options);
	}

	async getNetworkForEnvironment(
		environmentId: string,
		networkId: string,
		options?: SearchPaginationSortRequest
	): Promise<NetworkInspectDto> {
		const params = transformPaginationParams(options);
		return this.handleResponse(this.api.get(`/environments/${environmentId}/networks/${networkId}`, { params }));
	}

	async getNetworkTopology(environmentId?: string): Promise<NetworkTopologyDto> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/networks/topology`));
	}

	async createNetwork(name: string, options: NetworkCreateOptions, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const request: NetworkCreate = {
			name,
			options
		};
		return this.handleResponse(this.api.post(`/environments/${envId}/networks`, request));
	}

	async deleteNetwork(networkId: string, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.delete(`/environments/${envId}/networks/${networkId}`));
	}

	async connectContainer(
		networkId: string,
		request: { containerId: string; aliases?: string[]; ipv4Address?: string; ipv6Address?: string },
		environmentId?: string
	): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/networks/${networkId}/connect`, request));
	}

	async disconnectContainer(
		networkId: string,
		request: { containerId: string; force?: boolean },
		environmentId?: string
	): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/networks/${networkId}/disconnect`, request));
	}
}

export const networkService = new NetworkService();
