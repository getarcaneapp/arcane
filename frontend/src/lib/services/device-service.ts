import BaseAPIService from './api-service';
import type { Device, PairingCodeResponse, PairingCodeStatus, RenameDeviceRequest } from '$lib/types/device.type';

export default class DeviceAPIService extends BaseAPIService {
	async createPairingCode(): Promise<PairingCodeResponse> {
		return this.handleResponse(this.api.post('/devices/pairing-codes', {})) as Promise<PairingCodeResponse>;
	}

	async getPairingCodeStatus(id: string): Promise<PairingCodeStatus> {
		return this.handleResponse(this.api.get(`/devices/pairing-codes/${id}`)) as Promise<PairingCodeStatus>;
	}

	async listDevices(): Promise<Device[]> {
		return this.handleResponse(this.api.get('/devices')) as Promise<Device[]>;
	}

	async getDevice(id: string): Promise<Device> {
		return this.handleResponse(this.api.get(`/devices/${id}`)) as Promise<Device>;
	}

	async renameDevice(id: string, body: RenameDeviceRequest): Promise<Device> {
		return this.handleResponse(this.api.patch(`/devices/${id}`, body)) as Promise<Device>;
	}

	async revokeDevice(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/devices/${id}`)) as Promise<void>;
	}
}

export const deviceService = new DeviceAPIService();
