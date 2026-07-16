import BaseAPIService, { handleUnauthorizedResponseInternal } from './api-service';
import { streamCacheBuster } from '$lib/utils/streaming';
import type { OperationsState } from '$lib/types/operations';

class OperationsService extends BaseAPIService {
	async getState(environmentId: string): Promise<OperationsState> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/operations`));
	}

	async openStream(signal: AbortSignal, retry = false): Promise<Response> {
		const baseUrl = this.api.defaults.baseURL.replace(/\/+$/, '');
		const url = `${baseUrl}/operations/stream?_=${streamCacheBuster()}`;
		const response = await fetch(url, {
			credentials: 'include',
			headers: { Accept: 'application/x-json-stream' },
			signal
		});
		if (response.status === 401) {
			const action = await handleUnauthorizedResponseInternal('/operations/stream', retry);
			if (action === 'retry') return this.openStream(signal, true);
			if (action === 'redirect') return new Promise<Response>(() => {});
		}
		if (!response.ok) {
			throw new Error(`Operations stream failed with status ${response.status}`);
		}
		return response;
	}
}

export const operationsService = new OperationsService();
