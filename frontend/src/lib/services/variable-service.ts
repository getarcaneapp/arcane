import type { GlobalVariable, GlobalVariableMutationResponse } from '#lib/types/env.js';
import type { GlobalVariableCreate, GlobalVariableUpdate } from '#lib/types/ui/env.js';

import BaseAPIService from './api-service';

class VariableService extends BaseAPIService {
	async list(): Promise<GlobalVariable[]> {
		const response = await this.api.get('/variables');
		return response.data?.data ?? [];
	}

	async create(dto: GlobalVariableCreate): Promise<GlobalVariableMutationResponse> {
		const response = await this.api.post('/variables', dto);
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
		const response = await this.api.patch(`/variables/${encodeURIComponent(id)}`, dto);
		return response.data?.data ?? {};
	}

	async delete(id: string): Promise<GlobalVariableMutationResponse> {
		const response = await this.api.delete(`/variables/${encodeURIComponent(id)}`);
		return response.data?.data ?? {};
	}
}

export const variableService = new VariableService();
