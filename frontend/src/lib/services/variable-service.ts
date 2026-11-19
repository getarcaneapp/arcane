import type { GlobalVariable, GlobalVariableMutationResponse } from '#lib/types/env.js';
import type { GlobalVariableCreate, GlobalVariableUpdate } from '#lib/types/ui/env.js';

import BaseAPIService from './api-service';

class VariableService extends BaseAPIService {
	async list(): Promise<GlobalVariable[]> {
		const response = await this.api.get<{ data?: GlobalVariable[] }>('/variables');
		return response.data?.data ?? [];
	}

	async create(dto: GlobalVariableCreate): Promise<GlobalVariableMutationResponse> {
		const response = await this.api.post<{ data?: GlobalVariableMutationResponse }>('/variables', dto);
		return response.data?.data ?? {};
	}

	async createMany(dtos: GlobalVariableCreate[]): Promise<GlobalVariableMutationResponse> {
		let last: GlobalVariableMutationResponse = {};
		for (const dto of dtos) {
			last = await this.create(dto);
		}
		return last;
	}

	async update(id: string, dto: GlobalVariableUpdate): Promise<GlobalVariableMutationResponse> {
		const response = await this.api.patch<{ data?: GlobalVariableMutationResponse }>(`/variables/${encodeURIComponent(id)}`, dto);
		return response.data?.data ?? {};
	}

	async delete(id: string): Promise<GlobalVariableMutationResponse> {
		const response = await this.api.delete<{ data?: GlobalVariableMutationResponse }>(`/variables/${encodeURIComponent(id)}`);
		return response.data?.data ?? {};
	}
}

export const variableService = new VariableService();
