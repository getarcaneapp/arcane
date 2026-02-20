import { m } from '$lib/paraglide/messages';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import type { Project, ProjectStatusCounts } from '$lib/types/project.type';
import { transformPaginationParams } from '$lib/utils/params.util';
import BaseAPIService from './api-service';

export class ProjectService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getProjects(options?: SearchPaginationSortRequest): Promise<Paginated<Project>> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectsForEnvironment(envId, options);
	}

	async getProjectsForEnvironment(environmentId: string, options?: SearchPaginationSortRequest): Promise<Paginated<Project>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/projects`, { params });
		return res.data;
	}

	deployProject(projectId: string): Promise<Project>;
	deployProject(projectId: string, onLine: (data: any) => void): Promise<Project>;
	async deployProject(projectId: string, onLine?: (data: any) => void): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const url = `/api/environments/${envId}/projects/${projectId}/up`;

		const res = await fetch(url, { method: 'POST' });
		const status = String(res.status);
		if (!res.ok || !res.body) {
			throw new Error(m.progress_deploy_failed_to_start({ status }));
		}

		const reader = res.body.getReader();
		const decoder = new TextDecoder();
		let buffer = '';

		while (true) {
			const { value, done } = await reader.read();
			if (done) break;

			buffer += decoder.decode(value, { stream: true });
			const lines = buffer.split('\n');
			buffer = lines.pop() || '';

			for (const line of lines) {
				const trimmed = line.trim();
				if (!trimmed) continue;
				let obj: any;
				try {
					obj = JSON.parse(trimmed);
				} catch {
					continue;
				}

				onLine?.(obj);
				if (obj?.error) {
					throw new Error(typeof obj.error === 'string' ? obj.error : obj.error?.message || m.progress_deploy_failed());
				}
			}
		}

		// The deploy stream doesn't return the project object; fetch fresh details.
		return this.getProject(projectId);
	}

	async downProject(projectName: string): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectName}/down`));
	}

	async createProject(projectName: string, composeContent: string, envContent?: string): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const payload = {
			name: projectName,
			composeContent,
			envContent
		};
		return this.handleResponse(this.api.post(`/environments/${envId}/projects`, payload));
	}

	async getProject(projectId: string): Promise<Project> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectForEnvironment(envId, projectId);
	}

	async getProjectForEnvironment(environmentId: string, projectId: string): Promise<Project> {
		const response = await this.handleResponse<{ project?: Project; success?: boolean }>(
			this.api.get(`/environments/${environmentId}/projects/${projectId}`)
		);

		return response.project ? response.project : (response as Project);
	}

	async getProjectStatusCounts(): Promise<ProjectStatusCounts> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectStatusCountsForEnvironment(envId);
	}

	async getProjectStatusCountsForEnvironment(environmentId: string): Promise<ProjectStatusCounts> {
		const res = await this.api.get(`/environments/${environmentId}/projects/counts`);
		return res.data.data;
	}

	async updateProject(projectId: string, name?: string, composeContent?: string, envContent?: string): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const payload: Record<string, string> = {};
		if (name !== undefined) {
			payload.name = name;
		}
		if (composeContent !== undefined) {
			payload.composeContent = composeContent;
		}
		if (envContent !== undefined) {
			payload.envContent = envContent;
		}
		return this.handleResponse(this.api.put(`/environments/${envId}/projects/${projectId}`, payload));
	}

	async updateProjectIncludeFile(projectId: string, relativePath: string, content: string): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const payload = {
			relativePath,
			content
		};
		return this.handleResponse(this.api.put(`/environments/${envId}/projects/${projectId}/includes`, payload));
	}

	async restartProject(projectId: string): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/restart`));
	}

	async redeployProject(projectName: string): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectName}/redeploy`));
	}

	private async streamProjectPull(projectId: string, onLine?: (data: any) => void): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const url = `/api/environments/${envId}/projects/${projectId}/pull`;

		const res = await fetch(url, { method: 'POST' });
		if (!res.ok || !res.body) {
			throw new Error(`Failed to start project image pull (${res.status})`);
		}

		const reader = res.body.getReader();
		const decoder = new TextDecoder();
		let buffer = '';

		while (true) {
			const { value, done } = await reader.read();
			if (done) break;

			buffer += decoder.decode(value, { stream: true });
			const lines = buffer.split('\n');
			buffer = lines.pop() || '';

			for (const line of lines) {
				const trimmed = line.trim();
				if (!trimmed) continue;
				try {
					const obj = JSON.parse(trimmed);
					onLine?.(obj);
				} catch {
					// ignore malformed line
				}
			}
		}
	}

	pullProjectImages(projectId: string): Promise<void>;
	pullProjectImages(projectId: string, onLine: (data: any) => void): Promise<void>;
	async pullProjectImages(projectId: string, onLine?: (data: any) => void): Promise<void> {
		await this.streamProjectPull(projectId, onLine);
	}

	async destroyProject(projectName: string, removeVolumes = false, removeFiles = false): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(
			this.api.delete(`/environments/${envId}/projects/${projectName}/destroy`, {
				data: {
					removeVolumes,
					removeFiles
				}
			})
		);
	}
}

export const projectService = new ProjectService();
