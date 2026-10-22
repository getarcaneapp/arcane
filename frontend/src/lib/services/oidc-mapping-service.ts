import type { OidcGroupMapping, OidcGroupMappingSpec } from '#lib/types/auth.js';

import BaseAPIService from './api-service';

class OidcMappingAPIService extends BaseAPIService {
	async list(): Promise<OidcGroupMapping[]> {
		return this.handleResponse(this.api.get('/oidc/mappings'));
	}

	async create(mapping: OidcGroupMappingSpec): Promise<OidcGroupMapping> {
		return this.handleResponse(this.api.post('/oidc/mappings', mapping));
	}

	async update(id: string, mapping: OidcGroupMappingSpec): Promise<OidcGroupMapping> {
		return this.handleResponse(this.api.patch(`/oidc/mappings/${id}`, mapping));
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/oidc/mappings/${id}`));
	}
}

export const oidcMappingService = new OidcMappingAPIService();
