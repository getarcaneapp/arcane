import BaseAPIService from './api-service';
import type {
	GlobalVariable,
	GlobalVariableCreateDto,
	GlobalVariableUpdateDto,
	VariableMutationResponse
} from '$lib/types/variable';

class VariableService extends BaseAPIService {
	async list(): Promise<GlobalVariable[]> {
		const response = await this.api.get('/variables');
		return response.data?.data ?? [];
	}

	async create(dto: GlobalVariableCreateDto): Promise<VariableMutationResponse> {
		const response = await this.api.post('/variables', dto);
		return response.data?.data ?? {};
	}

	async createMany(dtos: GlobalVariableCreateDto[]): Promise<VariableMutationResponse> {
		let last: VariableMutationResponse = {};
		for (const dto of dtos) {
			last = await this.create(dto);
		}
		return last;
	}

	async update(id: string, dto: GlobalVariableUpdateDto): Promise<VariableMutationResponse> {
		const response = await this.api.put(`/variables/${encodeURIComponent(id)}`, dto);
		return response.data?.data ?? {};
	}

	async delete(id: string): Promise<VariableMutationResponse> {
		const response = await this.api.delete(`/variables/${encodeURIComponent(id)}`);
		return response.data?.data ?? {};
	}
}

export const variableService = new VariableService();
