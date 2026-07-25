import BaseAPIService, { handleUnauthorizedResponseInternal } from './api-service';
import { streamCacheBuster } from '#lib/utils/streaming';
import type { DashboardSnapshot } from '#lib/types/shared';

interface GetDashboardOptions {
	debugAllGood?: boolean;
}

class DashboardService extends BaseAPIService {
	async getDashboardForEnvironment(environmentId: string, options?: GetDashboardOptions): Promise<DashboardSnapshot> {
		const params = options?.debugAllGood ? { debugAllGood: 'true' } : undefined;
		return this.handleResponse(this.api.get(`/environments/${environmentId}/dashboard`, { params }));
	}

	getDashboardStreamUrl(debugAllGood = false): string {
		const baseUrl = this.api.defaults.baseURL.replace(/\/+$/, '');
		const params = new URLSearchParams();
		if (debugAllGood) {
			params.set('debugAllGood', 'true');
		}
		params.set('_', streamCacheBuster());
		const query = params.toString();
		return `${baseUrl}/dashboard/stream${query ? `?${query}` : ''}`;
	}

	async openDashboardStream(signal: AbortSignal, debugAllGood = false, retry = false): Promise<Response> {
		const response = await fetch(this.getDashboardStreamUrl(debugAllGood), {
			credentials: 'include',
			headers: { Accept: 'application/x-json-stream' },
			signal
		});
		if (response.status === 401) {
			// Nothing reads this body, so release its connection before deciding
			// what to do next.
			await response.body?.cancel().catch(() => {});
			const action = await handleUnauthorizedResponseInternal('/dashboard/stream', retry);
			if (action === 'retry') {
				return this.openDashboardStream(signal, debugAllGood, true);
			}
			// The document is being replaced (login redirect, or the reload that
			// follows a backend self-update). Never settling stops the caller
			// from opening a fresh stream against a page that is going away.
			if (action === 'redirect' || action === 'reload') {
				return new Promise<Response>(() => {});
			}
		}
		if (!response.ok) {
			throw new Error(`Dashboard stream failed with status ${response.status}`);
		}
		return response;
	}
}

export const dashboardService = new DashboardService();
