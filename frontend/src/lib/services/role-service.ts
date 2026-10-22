import type {
	EffectiveAccess,
	PermissionsManifest,
	Role,
	RoleBinding,
	RoleBindingCreate,
	RoleBindingCreateResult,
	RoleBindingFilter,
	RoleCreate,
	RoleUpdate
} from '#lib/types/auth.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class RoleAPIService extends BaseAPIService {
	async getRoles(options?: SearchPaginationSortRequest): Promise<Paginated<Role>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/roles', { params });
		return res.data;
	}

	async getAll(): Promise<Role[]> {
		// Unpaginated convenience for selects. Backend caps at a generous
		// limit (1000) which is well above any realistic role count.
		const res = await this.api.get('/roles', {
			params: { limit: -1, sort: 'name', order: 'asc' }
		});
		return res.data.data ?? [];
	}

	async get(id: string): Promise<Role> {
		return this.handleResponse(this.api.get(`/roles/${id}`)) as Promise<Role>;
	}

	async create(role: RoleCreate): Promise<Role> {
		return this.handleResponse(this.api.post('/roles', role)) as Promise<Role>;
	}

	async update(id: string, role: RoleUpdate): Promise<Role> {
		return this.handleResponse(this.api.patch(`/roles/${id}`, role)) as Promise<Role>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/roles/${id}`)) as Promise<void>;
	}

	async getPermissionsManifest(): Promise<PermissionsManifest> {
		return this.handleResponse(this.api.get('/roles/permissions')) as Promise<PermissionsManifest>;
	}

	async getBindings(filter: RoleBindingFilter, options?: SearchPaginationSortRequest): Promise<Paginated<RoleBinding>> {
		const params = { ...transformPaginationParams(options), ...filter };
		const res = await this.api.get('/roles/bindings', { params });
		return res.data;
	}

	async getBinding(id: string): Promise<RoleBinding> {
		return this.handleResponse(this.api.get(`/roles/bindings/${id}`));
	}

	async createBinding(binding: RoleBindingCreate): Promise<RoleBindingCreateResult> {
		return this.handleResponse(this.api.post('/roles/bindings', binding));
	}

	async deleteBinding(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/roles/bindings/${id}`));
	}

	async getUserEffectiveAccess(userId: string): Promise<EffectiveAccess> {
		return this.handleResponse(this.api.get(`/users/${userId}/effective-access`));
	}

	async getMyEffectiveAccess(): Promise<EffectiveAccess> {
		return this.handleResponse(this.api.get('/auth/me/effective-access'));
	}
}

export const roleService = new RoleAPIService();
