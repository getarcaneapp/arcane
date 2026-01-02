import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type {
	NetworkSummaryDto,
	NetworkUsageCounts,
	NetworkCreateRequest,
	NetworkCreateOptions,
	NetworkInspectDto
} from '$lib/types/network.type';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export type NetworksPaginatedResponse = Paginated<NetworkSummaryDto, NetworkUsageCounts>;

export class NetworkService extends BaseAPIService {
	async getNetworks(options?: SearchPaginationSortRequest): Promise<NetworksPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/networks`, { params });
		return res.data;
	}

	async getNetworkUsageCounts(): Promise<NetworkUsageCounts> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/networks/counts`);
		return res.data.data;
	}

	async getNetwork(networkId: string, options?: SearchPaginationSortRequest): Promise<NetworkInspectDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		return this.handleResponse(this.api.get(`/environments/${envId}/networks/${networkId}`, { params }));
	}

	async createNetwork(name: string, options: NetworkCreateOptions): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const request: NetworkCreateRequest = {
			name,
			options
		};
		return this.handleResponse(this.api.post(`/environments/${envId}/networks`, request));
	}

	async deleteNetwork(networkId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.delete(`/environments/${envId}/networks/${networkId}`));
	}
}

export const networkService = new NetworkService();
