import type {
	SecretSource,
	SecretSourceDefinition,
	SecretSourceInput,
	SecretSourceMutationResponse,
	SecretSourcePreset,
	SecretSourceTestInput,
	SecretSourceTestResult,
	SecretSourceUpdate
} from '#lib/types/secretsource.js';

import BaseAPIService from './api-service';

class SecretSourceService extends BaseAPIService {
	async list(): Promise<SecretSource[]> {
		const response = await this.api.get('/variables/sources');
		return response.data?.data ?? [];
	}

	async presets(): Promise<SecretSourcePreset[]> {
		const response = await this.api.get('/variables/source-presets');
		return response.data?.data ?? [];
	}

	async validateDefinition(definition: string): Promise<SecretSourceDefinition> {
		const response = await this.api.post('/variables/source-presets/validate', { definition });
		return response.data?.data;
	}

	async create(dto: SecretSourceInput): Promise<SecretSourceMutationResponse> {
		const response = await this.api.post('/variables/sources', dto);
		return response.data?.data ?? {};
	}

	async update(id: string, dto: SecretSourceUpdate): Promise<SecretSourceMutationResponse> {
		const response = await this.api.patch(`/variables/sources/${encodeURIComponent(id)}`, dto);
		return response.data?.data ?? {};
	}

	async delete(id: string): Promise<SecretSourceMutationResponse> {
		const response = await this.api.delete(`/variables/sources/${encodeURIComponent(id)}`);
		return response.data?.data ?? {};
	}

	async refresh(id: string): Promise<SecretSourceMutationResponse> {
		const response = await this.api.post(`/variables/sources/${encodeURIComponent(id)}/refresh`);
		return response.data?.data ?? {};
	}

	async test(dto: SecretSourceTestInput): Promise<SecretSourceTestResult> {
		const response = await this.api.post('/variables/sources/test', dto);
		return response.data?.data;
	}
}

export const secretSourceService = new SecretSourceService();
