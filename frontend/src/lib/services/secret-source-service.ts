import type {
	BindingTarget,
	ComposeRefsResult,
	ComposeService,
	ProjectSecretBinding,
	ProjectSecretBindingDto,
	ProjectSecretsCheck,
	SecretBrowseItem,
	SecretBrowseQuery,
	SecretSource,
	SecretSourceCreateDto,
	SecretSourceTestDto,
	SecretSourceTestResult,
	SecretSourceUpdateDto,
	SetupApplyRequest,
	SetupPlan,
	SetupPlanRequest,
	SetupResult,
	TargetKeys
} from '#lib/types/secret-source.js';

import BaseAPIService from './api-service';

class SecretSourceService extends BaseAPIService {
	async list(): Promise<SecretSource[]> {
		const response = await this.api.get('/secret-sources');
		return response.data?.data ?? [];
	}

	async create(dto: SecretSourceCreateDto): Promise<SecretSource> {
		return this.handleResponse(this.api.post('/secret-sources', dto));
	}

	async update(id: string, dto: SecretSourceUpdateDto): Promise<SecretSource> {
		return this.handleResponse(this.api.put(`/secret-sources/${encodeURIComponent(id)}`, dto));
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/secret-sources/${encodeURIComponent(id)}`));
	}

	async test(dto: SecretSourceTestDto): Promise<SecretSourceTestResult> {
		return this.handleResponse(this.api.post('/secret-sources/test', dto));
	}

	async browse(id: string, query: SecretBrowseQuery): Promise<SecretBrowseItem[]> {
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

	async targetKeys(sourceId: string, target: BindingTarget): Promise<TargetKeys> {
		return this.handleResponse(this.api.post(`/secret-sources/${encodeURIComponent(sourceId)}/keys`, { target }));
	}

	async composeServices(compose: string): Promise<ComposeService[]> {
		return this.handleResponse(this.api.post('/secret-sources/compose/services', { compose }));
	}

	// envKeys are added to env as KEY=${KEY}; leave them out to change compose only.
	async addComposeRefs(
		compose: string,
		assignments: Record<string, string[]>,
		env?: { content: string; keys: string[] }
	): Promise<ComposeRefsResult> {
		const body =
			env && env.keys.length > 0 ? { compose, assignments, env: env.content, envKeys: env.keys } : { compose, assignments };
		return this.handleResponse(this.api.post('/secret-sources/compose/refs', body));
	}

	async planSetup(environmentId: string, projectId: string, request: SetupPlanRequest): Promise<SetupPlan> {
		return this.handleResponse(this.api.post(`${this.bindingPath(environmentId, projectId)}/setup/plan`, request));
	}

	async applySetup(environmentId: string, projectId: string, request: SetupApplyRequest): Promise<SetupResult> {
		return this.handleResponse(this.api.post(`${this.bindingPath(environmentId, projectId)}/setup`, request));
	}

	async listBindings(environmentId: string, projectId: string): Promise<ProjectSecretBinding[]> {
		const response = await this.api.get(this.bindingPath(environmentId, projectId));
		return response.data?.data ?? [];
	}

	async createBinding(environmentId: string, projectId: string, dto: ProjectSecretBindingDto): Promise<ProjectSecretBinding> {
		return this.handleResponse(this.api.post(this.bindingPath(environmentId, projectId), dto));
	}

	async updateBinding(
		environmentId: string,
		projectId: string,
		bindingId: string,
		dto: ProjectSecretBindingDto
	): Promise<ProjectSecretBinding> {
		return this.handleResponse(
			this.api.put(`${this.bindingPath(environmentId, projectId)}/${encodeURIComponent(bindingId)}`, dto)
		);
	}

	async deleteBinding(environmentId: string, projectId: string, bindingId: string): Promise<void> {
		return this.handleResponse(this.api.delete(`${this.bindingPath(environmentId, projectId)}/${encodeURIComponent(bindingId)}`));
	}

	async checkBindings(environmentId: string, projectId: string): Promise<ProjectSecretsCheck> {
		return this.handleResponse(this.api.post(`${this.bindingPath(environmentId, projectId)}/check`, {}));
	}

	private bindingPath(environmentId: string, projectId: string): string {
		return `/environments/${encodeURIComponent(environmentId)}/projects/${encodeURIComponent(projectId)}/secrets`;
	}
}

export const secretSourceService = new SecretSourceService();
