import type { GoroutineProfile, PprofProfile } from '#lib/types/system.js';

import BaseAPIService from './api-service';

class DiagnosticsService extends BaseAPIService {
	async getGoroutinesForEnvironment(environmentId: string): Promise<GoroutineProfile> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/diagnostics/goroutines`));
	}

	async getProfileDumpForEnvironment(environmentId: string, name: 'goroutine' | 'heap'): Promise<string> {
		const res = await this.api.get<string>(`/agents/${environmentId}/debug/pprof/${name}`, {
			params: { debug: 2 },
			responseType: 'text'
		});
		return res.data;
	}

	async getProfileForEnvironment(environmentId: string, name: PprofProfile): Promise<Blob> {
		const timed = name === 'profile' || name === 'trace';
		const res = await this.api.get<Blob>(`/agents/${environmentId}/debug/pprof/${name}`, {
			params: timed ? { seconds: 30 } : undefined,
			responseType: 'blob',
			timeout: timed ? 90_000 : undefined
		});
		return res.data;
	}
}

export const diagnosticsService = new DiagnosticsService();
