import type { Paginated } from '#lib/types/base.js';
import type {
	Group,
	GroupCreate,
	GroupMember,
	GroupRoleAssignment,
	GroupUpdate,
	SetGroupMembers,
	SetGroupRoleAssignments
} from '#lib/types/group.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class GroupAPIService extends BaseAPIService {
	async getGroups(options?: SearchPaginationSortRequest): Promise<Paginated<Group>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get<Paginated<Group>>('/groups', { params });
		return res.data;
	}

	async get(id: string): Promise<Group> {
		return this.handleResponse(this.api.get(`/groups/${id}`)) as Promise<Group>;
	}

	async create(group: GroupCreate): Promise<Group> {
		return this.handleResponse(this.api.post('/groups', group)) as Promise<Group>;
	}

	async update(id: string, group: GroupUpdate): Promise<Group> {
		return this.handleResponse(this.api.patch(`/groups/${id}`, group)) as Promise<Group>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/groups/${id}`)) as Promise<void>;
	}

	async getMembers(id: string): Promise<GroupMember[]> {
		return this.handleResponse(this.api.get(`/groups/${id}/members`)) as Promise<GroupMember[]>;
	}

	async setMembers(id: string, payload: SetGroupMembers): Promise<GroupMember[]> {
		return this.handleResponse(this.api.put(`/groups/${id}/members`, payload)) as Promise<GroupMember[]>;
	}

	async getRoleAssignments(id: string): Promise<GroupRoleAssignment[]> {
		return this.handleResponse(this.api.get(`/groups/${id}/roles`)) as Promise<GroupRoleAssignment[]>;
	}

	async setRoleAssignments(id: string, payload: SetGroupRoleAssignments): Promise<GroupRoleAssignment[]> {
		return this.handleResponse(this.api.put(`/groups/${id}/roles`, payload)) as Promise<GroupRoleAssignment[]>;
	}
}

export const groupService = new GroupAPIService();
