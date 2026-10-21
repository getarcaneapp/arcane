import { m } from '#lib/paraglide/messages.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { DeployProjectOptions } from '#lib/types/project-deployment.js';
import type { ProjectWorkspaceFileDraft } from '#lib/types/project-workspace.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import type {
	Project,
	ProjectStatusCounts,
	ProjectUpdate,
	ProjectUpdateInfo,
	ProjectTag,
	ProjectTagColor,
	ProjectTagOption
} from '#lib/types/swarm.js';
import { readNdjsonStream } from '#lib/utils/streaming.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig, withSessionSignal } from './api-service';

class ProjectService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getProjects(options?: SearchPaginationSortRequest): Promise<Paginated<Project>> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectsForEnvironment(envId, options);
	}

	async getProjectsForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<Paginated<Project>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/projects`, { ...config, params });
		return res.data;
	}

	deployProject(
		projectId: string,
		mode: 'up' | 'redeploy',
		options?: DeployProjectOptions,
		environmentId?: string
	): Promise<Project>;
	deployProject(
		projectId: string,
		mode: 'up' | 'redeploy',
		onLine: (data: unknown) => void,
		options?: DeployProjectOptions,
		environmentId?: string
	): Promise<Project>;
	async deployProject(
		projectId: string,
		mode: 'up' | 'redeploy',
		onLineOrOptions?: ((data: unknown) => void) | DeployProjectOptions,
		optionsOrEnvironmentId?: DeployProjectOptions | string,
		maybeEnvironmentId?: string
	): Promise<Project> {
		const onLine = typeof onLineOrOptions === 'function' ? onLineOrOptions : undefined;
		const options = onLine
			? (optionsOrEnvironmentId as DeployProjectOptions | undefined)
			: (onLineOrOptions as DeployProjectOptions);
		const envId = await this.resolveEnvironmentId(onLine ? maybeEnvironmentId : (optionsOrEnvironmentId as string | undefined));
		const url = `/api/environments/${envId}/projects/${projectId}/${mode}`;

		await this.postProjectStream(url, options ?? {}, onLine, {
			startFailed: (status) => m.progress_deploy_failed_to_start({ status }),
			streamFailed: () => m.progress_deploy_failed()
		});

		// The deploy stream doesn't return the project object; fetch fresh details.
		return this.getProjectForEnvironment(envId, projectId);
	}

	async downProject(projectName: string, environmentId?: string): Promise<Project> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectName}/down`));
	}

	async createProject(
		projectName: string,
		composeContent: string,
		envContent?: string,
		workspaceFiles: ProjectWorkspaceFileDraft[] = [],
		tags: ProjectTag[] = [],
		environmentId?: string
	): Promise<Project> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const form = new FormData();
		const uploads: File[] = [];
		const fileChanges = workspaceFiles.map((file) => {
			if (file.isDirectory) return { operation: 'create_folder' as const, relativePath: file.relativePath };
			const uploadIndex = uploads.length;
			uploads.push(file.file ?? new File([file.content ?? ''], file.relativePath));
			return { operation: 'create_file' as const, relativePath: file.relativePath, uploadIndex };
		});
		form.append(
			'project',
			JSON.stringify({
				name: projectName,
				composeContent,
				envContent,
				tags: tags.map((tag) => tag.name),
				tagColors: Object.fromEntries(tags.map((tag) => [tag.name, tag.color]))
			})
		);
		form.append('manifest', JSON.stringify({ fileChanges }));
		for (const file of uploads) form.append('files', file, file.name);
		return this.handleResponse(this.api.post(`/environments/${envId}/projects`, form));
	}

	async checkUpdates(projectId: string, environmentId?: string): Promise<ProjectUpdateInfo> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/updater/projects/${encodeURIComponent(projectId)}/check`, {})
		);
	}

	async getProject(projectId: string): Promise<Project> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectForEnvironment(envId, projectId);
	}

	async getProjectForEnvironment(environmentId: string, projectId: string): Promise<Project> {
		const basePath = `/environments/${environmentId}/projects/${projectId}`;
		const [summary, compose, runtime, updates] = await Promise.all([
			this.getProjectSection(basePath),
			this.getProjectSection(`${basePath}/compose`),
			this.getProjectSection(`${basePath}/runtime`),
			this.getProjectSection(`${basePath}/updates`)
		]);

		return {
			...summary,
			...compose,
			...runtime,
			updateInfo: updates.updateInfo ?? compose.updateInfo ?? summary.updateInfo
		};
	}

	private async getProjectSection(path: string): Promise<Project> {
		const response = await this.handleResponse<{ project?: Project; success?: boolean } | Project>(
			this.api.get(path, { cache: 'no-store' })
		);
		return 'project' in response && response.project ? response.project : (response as Project);
	}

	private async readProjectStream(
		body: ReadableStream<Uint8Array>,
		onLine?: (data: any) => void,
		onMessage?: (data: any) => boolean | void
	): Promise<void> {
		await readNdjsonStream(body, onMessage, onLine);
	}

	private async postProjectStream(
		url: string,
		body: unknown,
		onLine: ((data: any) => void) | undefined,
		messages: { startFailed: (status: string) => string; streamFailed: () => string }
	): Promise<void> {
		const res = await fetch(url, {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify(body),
			signal: withSessionSignal()
		});
		if (!res.ok || !res.body) {
			throw new Error(messages.startFailed(String(res.status)));
		}

		await this.readProjectStream(res.body, onLine, (obj) => {
			if (obj?.error) {
				throw new Error(typeof obj.error === 'string' ? obj.error : obj.error?.message || messages.streamFailed());
			}
			// The terminal frame marks success; don't wait on the network EOF.
			return obj?.done === true;
		});
	}

	async getProjectStatusCounts(): Promise<ProjectStatusCounts> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectStatusCountsForEnvironment(envId);
	}

	async getProjectStatusCountsForEnvironment(environmentId: string, config?: APIRequestConfig): Promise<ProjectStatusCounts> {
		const res = await this.api.get(`/environments/${environmentId}/projects/counts`, config);
		return res.data.data;
	}

	async getProjectTagsForEnvironment(environmentId: string, config?: APIRequestConfig): Promise<ProjectTagOption[]> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/projects/tags`, config));
	}

	async updateProjectTag(
		projectId: string,
		name: string,
		attached: boolean,
		color?: ProjectTagColor,
		environmentId?: string
	): Promise<{ tags: ProjectTag[]; activityId?: string }> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.patch(`/environments/${envId}/projects/${projectId}/tags`, { name, attached, color }));
	}

	async updateProject(projectId: string, project: ProjectUpdate, environmentId?: string): Promise<Project> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.put(`/environments/${envId}/projects/${projectId}`, project));
	}

	async restartProject(projectId: string, services?: string[], environmentId?: string): Promise<unknown> {
		const envId = await this.resolveEnvironmentId(environmentId);
		let params: URLSearchParams | undefined;
		if (services && services.length > 0) {
			params = new URLSearchParams();
			for (const service of services) {
				params.append('services', service);
			}
		}
		return this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/restart`, undefined, { params }));
	}

	async archiveProject(projectId: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		await this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/archive`));
	}

	async unarchiveProject(projectId: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		await this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/unarchive`));
	}

	private async streamProjectPull(projectId: string, onLine?: (data: any) => void, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const url = `/api/environments/${envId}/projects/${projectId}/pull`;

		const res = await fetch(url, { method: 'POST', signal: withSessionSignal() });
		if (!res.ok || !res.body) {
			throw new Error(`Failed to start project image pull (${res.status})`);
		}

		await this.readProjectStream(res.body, onLine, (obj) => {
			if (obj?.error) {
				throw new Error(typeof obj.error === 'string' ? obj.error : obj.error?.message || m.images_pull_failed());
			}
			return obj?.done === true;
		});
	}

	buildProjectImages(
		projectId: string,
		options?: { services?: string[]; provider?: 'local' | 'depot'; push?: boolean; load?: boolean }
	): Promise<void>;
	buildProjectImages(
		projectId: string,
		options: { services?: string[]; provider?: 'local' | 'depot'; push?: boolean; load?: boolean } | undefined,
		onLine: (data: any) => void,
		environmentId?: string
	): Promise<void>;
	async buildProjectImages(
		projectId: string,
		options?: { services?: string[]; provider?: 'local' | 'depot'; push?: boolean; load?: boolean },
		onLine?: (data: any) => void,
		environmentId?: string
	): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const url = `/api/environments/${envId}/projects/${projectId}/build`;

		await this.postProjectStream(url, options || {}, onLine, {
			startFailed: (status) => `Failed to start project build (${status})`,
			streamFailed: () => m.build_failed()
		});
	}

	pullProjectImages(projectId: string): Promise<void>;
	pullProjectImages(projectId: string, onLine: (data: any) => void, environmentId?: string): Promise<void>;
	async pullProjectImages(projectId: string, onLine?: (data: any) => void, environmentId?: string): Promise<void> {
		await this.streamProjectPull(projectId, onLine, environmentId);
	}

	async destroyProject(projectName: string, removeVolumes = false, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
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
