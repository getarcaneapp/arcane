import BaseAPIService from './api-service';
import type { TemplateRegistry, Template, RemoteRegistry, TemplateContentData } from '$lib/types/template.type';
import type { Variable } from '$lib/types/variable.type';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export default class TemplateService extends BaseAPIService {
	async getTemplates(options?: SearchPaginationSortRequest): Promise<Paginated<Template>> {
		const params = transformPaginationParams(options);
		const response = await this.api.get('/templates', { params });
		return response.data;
	}

	async getAllTemplates(): Promise<Template[]> {
		const response = await this.api.get('/templates/all');
		return response.data?.data ?? [];
	}

	async getTemplateContent(id: string): Promise<TemplateContentData> {
		const encodedId = encodeURIComponent(id);
		const response = await this.api.get(`/templates/${encodedId}/content`);
		return response.data?.data;
	}

	async download(id: string): Promise<Template> {
		const response = await this.api.post(`/templates/${encodeURIComponent(id)}/download`);
		return response.data?.data;
	}

	async getDefaultTemplates(): Promise<{ composeTemplate: string; envTemplate: string }> {
		const response = await this.api.get('/templates/default');
		const data = response.data?.data;
		return {
			composeTemplate: data?.composeTemplate ?? '',
			envTemplate: data?.envTemplate ?? ''
		};
	}

	async saveDefaultTemplates(composeContent: string, envContent: string): Promise<void> {
		await this.api.post('/templates/default', {
			composeContent,
			envContent
		});
	}

	async getRegistries(): Promise<TemplateRegistry[]> {
		const response = await this.api.get('/templates/registries');
		const out = response.data?.data ?? response.data?.registries ?? response.data;
		return Array.isArray(out) ? out : [];
	}

	async addRegistry(registry: { name: string; url: string; description?: string; enabled: boolean }): Promise<TemplateRegistry> {
		const response = await this.api.post('/templates/registries', registry);
		return response.data?.data ?? response.data;
	}

	async updateRegistry(
		id: string,
		registry: {
			name: string;
			url: string;
			description?: string;
			enabled: boolean;
		}
	): Promise<void> {
		await this.api.put(`/templates/registries/${id}`, registry);
	}

	async fetchRegistry(url: string): Promise<RemoteRegistry> {
		const response = await this.api.get(`/templates/fetch?url=${encodeURIComponent(url)}`);
		const manifest = response.data?.data ?? response.data;
		if (!manifest || typeof manifest !== 'object' || !manifest.name || !Array.isArray(manifest.templates)) {
			throw new Error('Invalid registry format: missing required fields (name, templates)');
		}
		return manifest;
	}

	async deleteRegistry(id: string): Promise<void> {
		await this.api.delete(`/templates/registries/${id}`);
	}

	async deleteTemplate(id: string): Promise<void> {
		await this.api.delete(`/templates/${encodeURIComponent(id)}`);
	}

	async createTemplate(template: {
		name: string;
		description?: string;
		content: string;
		envContent?: string;
	}): Promise<Template> {
		const response = await this.api.post('/templates', {
			name: template.name,
			description: template.description || '',
			content: template.content,
			envContent: template.envContent || ''
		});
		return response.data?.data;
	}

	async getGlobalVariables(): Promise<Variable[]> {
		const response = await this.api.get('/templates/variables');
		return response.data?.data ?? [];
	}

	async updateGlobalVariables(variables: Variable[]): Promise<void> {
		await this.api.patch('/templates/variables', { variables });
	}
}

export const templateService = new TemplateService();
