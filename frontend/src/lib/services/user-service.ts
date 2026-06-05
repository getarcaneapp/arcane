import BaseAPIService from './api-service';
import type { User, CreateUser } from '$lib/types/auth';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
import { transformPaginationParams } from '$lib/utils/tables';

class UserAPIService extends BaseAPIService {
	async getUsers(options?: SearchPaginationSortRequest): Promise<Paginated<User>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/users', { params });
		return res.data;
	}

	async get(id: string): Promise<User> {
		return this.handleResponse(this.api.get(`/users/${id}`)) as Promise<User>;
	}

	async getCurrentUser(): Promise<User> {
		return this.handleResponse(this.api.get(`/auth/me`)) as Promise<User>;
	}

	async create(user: CreateUser): Promise<User> {
		return this.handleResponse(this.api.post('/users', user)) as Promise<User>;
	}

	async update(id: string, user: Partial<User>): Promise<User> {
		return this.handleResponse(this.api.put(`/users/${id}`, user)) as Promise<User>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/users/${id}`)) as Promise<void>;
	}

	async changePassword(data: { currentPassword: string; newPassword: string }): Promise<void> {
		return this.handleResponse(this.api.post('/auth/password', data)) as Promise<void>;
	}

	async logoutAllOtherSessions(): Promise<void> {
		return this.handleResponse(this.api.post('/auth/sessions/logout-all')) as Promise<void>;
	}

	async updateMyProfile(data: { displayName?: string; email?: string; locale?: string }): Promise<User> {
		return this.handleResponse(this.api.put('/auth/me/profile', data)) as Promise<User>;
	}
}

export const userService = new UserAPIService();
