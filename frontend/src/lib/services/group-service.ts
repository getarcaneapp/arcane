import type { Group, GroupCreate, GroupMember, GroupUpdate } from '#lib/types/auth.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class GroupAPIService extends BaseAPIService {
	async getGroups(options?: SearchPaginationSortRequest): Promise<Paginated<Group>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/groups', { params });
		return res.data;
	}

	async getAll(): Promise<Group[]> {
		const res = await this.api.get('/groups', {
			params: { limit: -1, sort: 'name', order: 'asc' }
		});
		return res.data.data ?? [];
	}

	async get(id: string): Promise<Group> {
		return this.handleResponse(this.api.get(`/groups/${id}`));
	}

	async create(group: GroupCreate): Promise<Group> {
		return this.handleResponse(this.api.post('/groups', group));
	}

	async update(id: string, group: GroupUpdate): Promise<Group> {
		return this.handleResponse(this.api.patch(`/groups/${id}`, group));
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/groups/${id}`));
	}

	async getMembers(id: string): Promise<GroupMember[]> {
		return this.handleResponse(this.api.get(`/groups/${id}/members`));
	}

	async addMembers(id: string, userIds: string[]): Promise<GroupMember[]> {
		return this.handleResponse(this.api.post(`/groups/${id}/members`, { userIds }));
	}

	async removeMember(id: string, userId: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/groups/${id}/members/${userId}`));
	}
}

export const groupService = new GroupAPIService();
