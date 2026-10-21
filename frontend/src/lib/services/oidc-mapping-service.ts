import type { OidcRoleMapping, OidcRoleMappingCreate, OidcRoleMappingUpdate } from '#lib/types/auth.js';

import BaseAPIService from './api-service';

class OidcMappingAPIService extends BaseAPIService {
	async list(): Promise<OidcRoleMapping[]> {
		return this.handleResponse(this.api.get('/oidc/mappings')) as Promise<OidcRoleMapping[]>;
	}

	async create(mapping: OidcRoleMappingCreate): Promise<OidcRoleMapping> {
		return this.handleResponse(this.api.post('/oidc/mappings', mapping)) as Promise<OidcRoleMapping>;
	}

	async update(id: string, mapping: OidcRoleMappingUpdate): Promise<OidcRoleMapping> {
		return this.handleResponse(this.api.patch(`/oidc/mappings/${id}`, mapping)) as Promise<OidcRoleMapping>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/oidc/mappings/${id}`)) as Promise<void>;
	}
}

export const oidcMappingService = new OidcMappingAPIService();
