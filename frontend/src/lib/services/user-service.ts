import type { ProfileUpdate, User, UserCreate, UserUpdate } from '#lib/types/auth.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

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

	async create(user: UserCreate): Promise<User> {
		return this.handleResponse(this.api.post('/users', user)) as Promise<User>;
	}

	async update(id: string, user: UserUpdate): Promise<User> {
		return this.handleResponse(this.api.patch(`/users/${id}`, user)) as Promise<User>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/users/${id}`)) as Promise<void>;
	}

	async changePassword(data: { currentPassword: string; newPassword: string }): Promise<void> {
		return this.handleResponse(this.api.post('/auth/password', data)) as Promise<void>;
	}

	async logoutAllOtherSessions(): Promise<void> {
		return this.handleResponse(this.api.delete('/auth/sessions')) as Promise<void>;
	}

	async updateMyProfile(data: ProfileUpdate): Promise<User> {
		return this.handleResponse(this.api.patch('/auth/me/profile', data)) as Promise<User>;
	}

	async uploadMyAvatar(file: File): Promise<User> {
		const formData = new FormData();
		formData.append('file', file);
		return this.handleResponse(this.api.post('/auth/me/avatar', formData)) as Promise<User>;
	}

	async deleteMyAvatar(): Promise<User> {
		return this.handleResponse(this.api.delete('/auth/me/avatar')) as Promise<User>;
	}
}

export const userService = new UserAPIService();
