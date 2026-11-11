import type { MessageResponse } from '#lib/types/base.js';
import type {
	NetworkUsageCounts,
	NetworkCreateOptions,
	NetworkCreateResponse,
	NetworkInspect,
	NetworkTopology
} from '#lib/types/network.js';
import type { NetworkCreate, NetworksPaginatedResponse } from '#lib/types/ui/network.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class NetworkService extends BaseAPIService {
	async getNetworksForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<NetworksPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/agents/${environmentId}/networks`, { ...config, params });
		return res.data;
	}

	async getNetworkUsageCounts(environmentId: string, config?: APIRequestConfig): Promise<NetworkUsageCounts> {
		const res = await this.api.get(`/agents/${environmentId}/networks/counts`, config);
		return res.data.data;
	}

	async getNetworkForEnvironment(
		environmentId: string,
		networkId: string,
		options?: SearchPaginationSortRequest
	): Promise<NetworkInspect> {
		const params = transformPaginationParams(options);
		return this.handleResponse(this.api.get(`/agents/${environmentId}/networks/${networkId}`, { params }));
	}

	async getNetworkTopology(environmentId: string, config?: APIRequestConfig): Promise<NetworkTopology> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/networks/topology`, config));
	}

	async createNetwork(name: string, options: NetworkCreateOptions, environmentId: string): Promise<NetworkCreateResponse> {
		const request: NetworkCreate = {
			name,
			options
		};
		return this.handleResponse(this.api.post(`/agents/${environmentId}/networks`, request));
	}

	async deleteNetwork(networkId: string, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(this.api.delete(`/agents/${environmentId}/networks/${networkId}`));
	}

	async connectContainer(
		networkId: string,
		request: { containerId: string; aliases?: string[]; ipv4Address?: string; ipv6Address?: string },
		environmentId: string
	): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/networks/${networkId}`, request, { params: { action: 'connect' } })
		);
	}

	async disconnectContainer(
		networkId: string,
		request: { containerId: string; force?: boolean },
		environmentId: string
	): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/networks/${networkId}`, request, { params: { action: 'disconnect' } })
		);
	}
}

export const networkService = new NetworkService();
