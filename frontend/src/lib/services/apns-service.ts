import BaseAPIService from './api-service';
import type { ApnsStatus } from '#lib/types/apns.js';

class ApnsService extends BaseAPIService {
	async getStatus(): Promise<ApnsStatus> {
		return this.handleResponse(this.api.get('/apns/status'));
	}

	async testDevice(id: string): Promise<void> {
		await this.api.post(`/apns/devices/${id}/test`);
	}

	async deleteDevice(id: string): Promise<void> {
		await this.api.delete(`/apns/devices/${id}`);
	}
}

export const apnsService = new ApnsService();
