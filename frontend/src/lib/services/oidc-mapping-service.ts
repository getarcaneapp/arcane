import BaseAPIService from './api-service';
import type { OidcRoleMapping, CreateOidcRoleMapping, UpdateOidcRoleMapping } from '$lib/types/auth';

class OidcMappingAPIService extends BaseAPIService {
	async list(): Promise<OidcRoleMapping[]> {
		return this.handleResponse(this.api.get('/oidc/role-mappings')) as Promise<OidcRoleMapping[]>;
	}

	async create(mapping: CreateOidcRoleMapping): Promise<OidcRoleMapping> {
		return this.handleResponse(this.api.post('/oidc/role-mappings', mapping)) as Promise<OidcRoleMapping>;
	}

	async update(id: string, mapping: UpdateOidcRoleMapping): Promise<OidcRoleMapping> {
		return this.handleResponse(this.api.put(`/oidc/role-mappings/${id}`, mapping)) as Promise<OidcRoleMapping>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/oidc/role-mappings/${id}`)) as Promise<void>;
	}
}

export const oidcMappingService = new OidcMappingAPIService();
