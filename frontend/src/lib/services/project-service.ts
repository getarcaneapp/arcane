import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { Project, ProjectStatusCounts } from '$lib/types/project.type';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';
import { m } from '$lib/paraglide/messages';

export class ProjectService extends BaseAPIService {
	async getProjects(options?: SearchPaginationSortRequest): Promise<Paginated<Project>> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/projects`, { params });
		return res.data;
	}

	deployProject(projectId: string): Promise<Project>;
	deployProject(projectId: string, onLine: (data: any) => void): Promise<Project>;
	async deployProject(projectId: string, onLine?: (data: any) => void): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const url = `/api/environments/${envId}/projects/${projectId}/up`;

		const res = await fetch(url, { method: 'POST' });
		if (!res.ok || !res.body) {
			throw new Error(m.progress_deploy_failed_to_start({ status: String(res.status) }));
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
		const envId = await environmentStore.getCurrentEnvironmentId();
		const response = await this.handleResponse<{ project?: Project; success?: boolean }>(
			this.api.get(`/environments/${envId}/projects/${projectId}`)
		);

		return response.project ? response.project : (response as Project);
	}

	async getProjectStatusCounts(): Promise<ProjectStatusCounts> {
		const envId = await environmentStore.getCurrentEnvironmentId();

		const res = await this.api.get(`/environments/${envId}/projects/counts`);
		return res.data.data;
	}

	async updateProject(projectId: string, name: string, composeContent: string, envContent?: string): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const payload = {
			name,
			composeContent,
			envContent
		};
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

	private isDownloadingStatus(status?: string): boolean {
		if (!status) return false;
		const s = status.toLowerCase();
		return (
			s.includes('downloading') ||
			s.includes('extracting') ||
			s.includes('pull complete') ||
			s.includes('download complete') ||
			s.includes('pulling fs layer')
		);
	}

	private async streamProjectPull(projectId: string, onLine?: (data: any) => void): Promise<boolean> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const url = `/api/environments/${envId}/projects/${projectId}/pull`;

		const res = await fetch(url, { method: 'POST' });
		if (!res.ok || !res.body) {
			throw new Error(`Failed to start project image pull (${res.status})`);
		}

		const reader = res.body.getReader();
		const decoder = new TextDecoder();
		let buffer = '';
		let pulled = false;

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

					// Detect if any actual download happened
					if (!pulled) {
						const status = obj?.status as string | undefined;
						const total = obj?.progressDetail?.total as number | undefined;
						if (this.isDownloadingStatus(status) || (typeof total === 'number' && total > 0)) {
							pulled = true;
						}
					}

					onLine?.(obj);
				} catch {
					// ignore malformed line
				}
			}
		}
		return pulled;
	}

	pullProjectImages(projectId: string): Promise<void>;
	pullProjectImages(projectId: string, onLine: (data: any) => void): Promise<void>;
	async pullProjectImages(projectId: string, onLine?: (data: any) => void): Promise<void> {
		await this.streamProjectPull(projectId, onLine);
	}

	async deployProjectMaybePull(
		projectId: string,
		onPullLine?: (data: any) => void,
		onDeployLine?: (data: any) => void
	): Promise<{ pulled: boolean; project: Project }> {
		const pulled = await this.streamProjectPull(projectId, onPullLine);
		const project = onDeployLine ? await this.deployProject(projectId, onDeployLine) : await this.deployProject(projectId);
		return { pulled, project };
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
