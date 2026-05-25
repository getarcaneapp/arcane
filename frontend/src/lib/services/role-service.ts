import BaseAPIService from './api-service';
import type { Role, CreateRole, UpdateRole, RoleAssignment, SetUserAssignments, PermissionsManifest } from '$lib/types/auth';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
import { transformPaginationParams } from '$lib/utils/tables';

export default class RoleAPIService extends BaseAPIService {
	async getRoles(options?: SearchPaginationSortRequest): Promise<Paginated<Role>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/roles', { params });
		return res.data;
	}

	async getAll(): Promise<Role[]> {
		// Unpaginated convenience for selects. Backend caps at a generous
		// limit (1000) which is well above any realistic role count.
		const res = await this.api.get('/roles', {
			params: { limit: 1000, sort: 'name', order: 'asc' }
		});
		return res.data.data ?? [];
	}

	async get(id: string): Promise<Role> {
		return this.handleResponse(this.api.get(`/roles/${id}`)) as Promise<Role>;
	}

	async create(role: CreateRole): Promise<Role> {
		return this.handleResponse(this.api.post('/roles', role)) as Promise<Role>;
	}

	async update(id: string, role: UpdateRole): Promise<Role> {
		return this.handleResponse(this.api.put(`/roles/${id}`, role)) as Promise<Role>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/roles/${id}`)) as Promise<void>;
	}

	async getPermissionsManifest(): Promise<PermissionsManifest> {
		return this.handleResponse(this.api.get('/roles/available-permissions')) as Promise<PermissionsManifest>;
	}

	async getUserAssignments(userId: string): Promise<RoleAssignment[]> {
		return this.handleResponse(this.api.get(`/users/${userId}/role-assignments`)) as Promise<RoleAssignment[]>;
	}

	async setUserAssignments(userId: string, payload: SetUserAssignments): Promise<RoleAssignment[]> {
		return this.handleResponse(this.api.put(`/users/${userId}/role-assignments`, payload)) as Promise<RoleAssignment[]>;
	}
}

export const roleService = new RoleAPIService();
