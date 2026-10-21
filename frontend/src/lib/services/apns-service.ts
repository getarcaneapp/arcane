import type { ApnsStatus } from '#lib/types/apns.js';

import BaseAPIService from './api-service';

class ApnsService extends BaseAPIService {
	async getStatus(): Promise<ApnsStatus> {
		return this.handleResponse(this.api.get('/apns/status'));
	}

	async testDevice(id: string): Promise<void> {
		await this.api.post(`/apns/devices/${id}`, undefined, { params: { action: 'test' } });
	}

	async deleteDevice(id: string): Promise<void> {
		await this.api.delete(`/apns/devices/${id}`);
	}
}

export const apnsService = new ApnsService();
