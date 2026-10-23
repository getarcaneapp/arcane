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
	async getNetworksForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<NetworksPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/networks`, { ...config, params });
		return res.data;
	}

	async getNetworkUsageCounts(environmentId: string, config?: APIRequestConfig): Promise<NetworkUsageCounts> {
		const res = await this.api.get(`/environments/${environmentId}/networks/counts`, config);
		return res.data.data;
	}

	async getNetworkForEnvironment(
		environmentId: string,
		networkId: string,
		options?: SearchPaginationSortRequest
	): Promise<NetworkInspectDto> {
		const params = transformPaginationParams(options);
		return this.handleResponse(this.api.get(`/environments/${environmentId}/networks/${networkId}`, { params }));
	}

	async getNetworkTopology(environmentId: string, config?: APIRequestConfig): Promise<NetworkTopologyDto> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/networks/topology`, config));
	}

	async createNetwork(name: string, options: NetworkCreateOptions, environmentId: string): Promise<any> {
		const request: NetworkCreate = {
			name,
			options
		};
		return this.handleResponse(this.api.post(`/environments/${environmentId}/networks`, request));
	}

	async deleteNetwork(networkId: string, environmentId: string): Promise<any> {
		return this.handleResponse(this.api.delete(`/environments/${environmentId}/networks/${networkId}`));
	}

	async connectContainer(
		networkId: string,
		request: { containerId: string; aliases?: string[]; ipv4Address?: string; ipv6Address?: string },
		environmentId: string
	): Promise<any> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/networks/${networkId}`, request, { params: { action: 'connect' } })
		);
	}

	async disconnectContainer(
		networkId: string,
		request: { containerId: string; force?: boolean },
		environmentId: string
	): Promise<any> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/networks/${networkId}`, request, { params: { action: 'disconnect' } })
		);
	}
}

export const networkService = new NetworkService();
