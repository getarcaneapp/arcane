import type {
	SecretSourceBindingTarget,
	SecretSourceComposeRefsResult,
	SecretSourceComposeService,
	SecretSourceBinding,
	SecretSourceBindingUpsertRequest,
	SecretSourceProjectCheckResult,
	SecretSourceBrowseItem,
	SecretSourceBrowseQuery,
	SecretSource,
	SecretSourceCreateRequest,
	SecretSourceTestRequest,
	SecretSourceTestResult,
	SecretSourceUpdateRequest,
	SecretSourceSetupApplyRequest,
	SecretSourceSetupPlan,
	SecretSourceSetupPlanRequest,
	SecretSourceSetupResult,
	SecretSourceTargetKeys
} from '#lib/types/secretsource.js';

import BaseAPIService from './api-service';

class SecretSourceService extends BaseAPIService {
	async list(): Promise<SecretSource[]> {
		const response = await this.api.get('/secret-sources');
		return response.data?.data ?? [];
	}

	async create(dto: SecretSourceCreateRequest): Promise<SecretSource> {
		return this.handleResponse(this.api.post('/secret-sources', dto));
	}

	async update(id: string, dto: SecretSourceUpdateRequest): Promise<SecretSource> {
		return this.handleResponse(this.api.put(`/secret-sources/${encodeURIComponent(id)}`, dto));
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/secret-sources/${encodeURIComponent(id)}`));
	}

	async test(dto: SecretSourceTestRequest): Promise<SecretSourceTestResult> {
		return this.handleResponse(this.api.post('/secret-sources/test', dto));
	}

	async browse(id: string, query: SecretSourceBrowseQuery): Promise<SecretSourceBrowseItem[]> {
		const params = {
			kind: query.kind,
			projectId: query.projectId || undefined,
			environment: query.environment || undefined,
			path: query.path || undefined,
			mount: query.mount || undefined,
			kvVersion: query.kvVersion || undefined,
			vaultId: query.vaultId || undefined
		};
		const response = await this.api.get(`/secret-sources/${encodeURIComponent(id)}/browse`, { params });
		return response.data?.data ?? [];
	}

	async targetKeys(sourceId: string, target: SecretSourceBindingTarget): Promise<SecretSourceTargetKeys> {
		return this.handleResponse(this.api.post(`/secret-sources/${encodeURIComponent(sourceId)}/keys`, { target }));
	}

	async composeServices(compose: string): Promise<SecretSourceComposeService[]> {
		return this.handleResponse(this.api.post('/secret-sources/compose/services', { compose }));
	}

	// envKeys are added to env as KEY=${KEY}; leave them out to change compose only.
	async addComposeRefs(
		compose: string,
		assignments: Record<string, string[]>,
		env?: { content: string; keys: string[] }
	): Promise<SecretSourceComposeRefsResult> {
		const body =
			env && env.keys.length > 0 ? { compose, assignments, env: env.content, envKeys: env.keys } : { compose, assignments };
		return this.handleResponse(this.api.post('/secret-sources/compose/refs', body));
	}

	async planSetup(
		environmentId: string,
		projectId: string,
		request: SecretSourceSetupPlanRequest
	): Promise<SecretSourceSetupPlan> {
		return this.handleResponse(this.api.post(`${this.bindingPath(environmentId, projectId)}/setup/plan`, request));
	}

	async applySetup(
		environmentId: string,
		projectId: string,
		request: SecretSourceSetupApplyRequest
	): Promise<SecretSourceSetupResult> {
		return this.handleResponse(this.api.post(`${this.bindingPath(environmentId, projectId)}/setup`, request));
	}

	async listBindings(environmentId: string, projectId: string): Promise<SecretSourceBinding[]> {
		const response = await this.api.get(this.bindingPath(environmentId, projectId));
		return response.data?.data ?? [];
	}

	async createBinding(
		environmentId: string,
		projectId: string,
		dto: SecretSourceBindingUpsertRequest
	): Promise<SecretSourceBinding> {
		return this.handleResponse(this.api.post(this.bindingPath(environmentId, projectId), dto));
	}

	async updateBinding(
		environmentId: string,
		projectId: string,
		bindingId: string,
		dto: SecretSourceBindingUpsertRequest
	): Promise<SecretSourceBinding> {
		return this.handleResponse(
			this.api.put(`${this.bindingPath(environmentId, projectId)}/${encodeURIComponent(bindingId)}`, dto)
		);
	}

	async deleteBinding(environmentId: string, projectId: string, bindingId: string): Promise<void> {
		return this.handleResponse(this.api.delete(`${this.bindingPath(environmentId, projectId)}/${encodeURIComponent(bindingId)}`));
	}

	async checkBindings(environmentId: string, projectId: string): Promise<SecretSourceProjectCheckResult> {
		return this.handleResponse(this.api.post(`${this.bindingPath(environmentId, projectId)}/check`, {}));
	}

	private bindingPath(environmentId: string, projectId: string): string {
		return `/environments/${encodeURIComponent(environmentId)}/projects/${encodeURIComponent(projectId)}/secrets`;
	}
}

export const secretSourceService = new SecretSourceService();
