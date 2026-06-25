import { m } from '$lib/paraglide/messages';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
import type { IncludeFile, Project, ProjectStatusCounts } from '$lib/types/swarm';
import type { ProjectFileChange, ProjectFileDraft } from '$lib/types/project-files';
import { readNdjsonStream } from '$lib/utils/streaming';
import { transformPaginationParams } from '$lib/utils/tables';
import BaseAPIService from './api-service';

export type DeployProjectOptions = {
	pullPolicy?: 'missing' | 'always' | 'never';
	forceRecreate?: boolean;
};

class ProjectService extends BaseAPIService {
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

	deployProject(projectId: string, options?: DeployProjectOptions): Promise<Project>;
	deployProject(projectId: string, onLine: (data: any) => void, options?: DeployProjectOptions): Promise<Project>;
	async deployProject(
		projectId: string,
		onLineOrOptions?: ((data: any) => void) | DeployProjectOptions,
		maybeOptions?: DeployProjectOptions
	): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const url = `/api/environments/${envId}/projects/${projectId}/up`;
		const onLine = typeof onLineOrOptions === 'function' ? onLineOrOptions : undefined;
		const options = typeof onLineOrOptions === 'function' ? maybeOptions : onLineOrOptions;

		const res = await fetch(url, {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify(options ?? {})
		});
		const status = String(res.status);
		if (!res.ok || !res.body) {
			throw new Error(m.progress_deploy_failed_to_start({ status }));
		}

		await this.readProjectStream(res.body, onLine, (obj) => {
			if (obj?.error) {
				throw new Error(typeof obj.error === 'string' ? obj.error : obj.error?.message || m.progress_deploy_failed());
			}
		});

		// The deploy stream doesn't return the project object; fetch fresh details.
		return this.getProject(projectId);
	}

	async downProject(projectName: string): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectName}/down`));
	}

	async createProject(
		projectName: string,
		composeContent: string,
		envContent?: string,
		projectFiles?: ProjectFileDraft[]
	): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const payload = {
			name: projectName,
			composeContent,
			envContent,
			projectFiles
		};
		return this.handleResponse(this.api.post(`/environments/${envId}/projects`, payload));
	}

	async getProject(projectId: string): Promise<Project> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectForEnvironment(envId, projectId);
	}

	async getProjectForEnvironment(environmentId: string, projectId: string): Promise<Project> {
		const basePath = `/environments/${environmentId}/projects/${projectId}`;
		const [summary, compose, files, runtime, updates] = await Promise.all([
			this.getProjectSection(basePath),
			this.getProjectSection(`${basePath}/compose`),
			this.getProjectSection(`${basePath}/files`),
			this.getProjectSection(`${basePath}/runtime`),
			this.getProjectSection(`${basePath}/updates`)
		]);

		return {
			...summary,
			...compose,
			...files,
			...runtime,
			updateInfo: updates.updateInfo ?? compose.updateInfo ?? summary.updateInfo
		};
	}

	private async getProjectSection(path: string): Promise<Project> {
		const response = await this.handleResponse<{ project?: Project; success?: boolean } | Project>(this.api.get(path));
		return 'project' in response && response.project ? response.project : (response as Project);
	}

	private async readProjectStream(
		body: ReadableStream<Uint8Array>,
		onLine?: (data: any) => void,
		onMessage?: (data: any) => void
	): Promise<void> {
		await readNdjsonStream(body, onMessage, onLine);
	}

	async getProjectFile(projectId: string, relativePath: string): Promise<IncludeFile> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectFileForEnvironment(envId, projectId, relativePath);
	}

	async getProjectFileForEnvironment(environmentId: string, projectId: string, relativePath: string): Promise<IncludeFile> {
		return this.handleResponse<IncludeFile>(
			this.api.get(`/environments/${environmentId}/projects/${projectId}/file`, {
				params: { relativePath }
			})
		);
	}

	async getProjectStatusCounts(): Promise<ProjectStatusCounts> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectStatusCountsForEnvironment(envId);
	}

	async getProjectStatusCountsForEnvironment(environmentId: string): Promise<ProjectStatusCounts> {
		const res = await this.api.get(`/environments/${environmentId}/projects/counts`);
		return res.data.data;
	}

	async updateProject(
		projectId: string,
		name?: string,
		composeContent?: string,
		envContent?: string,
		fileTreeRevision?: string,
		fileChanges?: ProjectFileChange[]
	): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const payload: {
			name?: string;
			composeContent?: string;
			envContent?: string;
			fileTreeRevision?: string;
			fileChanges?: ProjectFileChange[];
		} = {};
		if (name !== undefined) {
			payload.name = name;
		}
		if (composeContent !== undefined) {
			payload.composeContent = composeContent;
		}
		if (envContent !== undefined) {
			payload.envContent = envContent;
		}
		if (fileChanges && fileChanges.length > 0) {
			payload.fileTreeRevision = fileTreeRevision;
			payload.fileChanges = fileChanges;
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

	async restartProject(projectId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/restart`));
	}

	async archiveProject(projectId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/archive`));
	}

	async unarchiveProject(projectId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/unarchive`));
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

		await this.readProjectStream(res.body, onLine);
	}

	buildProjectImages(
		projectId: string,
		options?: { services?: string[]; provider?: 'local' | 'depot'; push?: boolean; load?: boolean }
	): Promise<void>;
	buildProjectImages(
		projectId: string,
		options: { services?: string[]; provider?: 'local' | 'depot'; push?: boolean; load?: boolean } | undefined,
		onLine: (data: any) => void
	): Promise<void>;
	async buildProjectImages(
		projectId: string,
		options?: { services?: string[]; provider?: 'local' | 'depot'; push?: boolean; load?: boolean },
		onLine?: (data: any) => void
	): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const url = `/api/environments/${envId}/projects/${projectId}/build`;

		const res = await fetch(url, {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify(options || {})
		});
		if (!res.ok || !res.body) {
			throw new Error(`Failed to start project build (${res.status})`);
		}

		await this.readProjectStream(res.body, onLine, (obj) => {
			if (obj?.error) {
				throw new Error(typeof obj.error === 'string' ? obj.error : obj.error?.message || m.build_failed());
			}
		});
	}

	pullProjectImages(projectId: string): Promise<void>;
	pullProjectImages(projectId: string, onLine: (data: any) => void): Promise<void>;
	async pullProjectImages(projectId: string, onLine?: (data: any) => void): Promise<void> {
		await this.streamProjectPull(projectId, onLine);
	}

	async destroyProject(projectName: string, removeVolumes = false): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(
			this.api.delete(`/environments/${envId}/projects/${projectName}/destroy`, {
				data: {
					removeVolumes
				}
			})
		);
	}
}

export const projectService = new ProjectService();
