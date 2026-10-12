import type { GoroutineProfile } from '#lib/types/diagnostics.js';

import BaseAPIService from './api-service';

class DiagnosticsService extends BaseAPIService {
	async getGoroutinesForEnvironment(environmentId: string): Promise<GoroutineProfile> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/diagnostics/goroutines`));
	}
}

export const diagnosticsService = new DiagnosticsService();
