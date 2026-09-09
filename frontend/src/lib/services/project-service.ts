import { m } from '#lib/paraglide/messages.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import type {
	Project,
	ProjectStatusCounts,
	ProjectUpdateInfo,
	ProjectTag,
	ProjectTagColor,
	ProjectTagOption
} from '#lib/types/swarm.js';
import type { ProjectWorkspaceFileDraft } from '#lib/types/project-workspace.js';
import { readNdjsonStream } from '#lib/utils/streaming.js';
import { transformPaginationParams } from '#lib/utils/tables.js';
import type { DeployProjectOptions } from '#lib/types/project-deployment.js';
import BaseAPIService from './api-service';

class ProjectService extends BaseAPIService {
	async getProjectsForEnvironment(environmentId: string, options?: SearchPaginationSortRequest): Promise<Paginated<Project>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get<Paginated<Omit<Project, 'environmentId'>>>(`/environments/${environmentId}/projects`, {
			params
		});
		return { ...res.data, data: res.data.data.map((project) => ({ ...project, environmentId })) };
	}

	deployProject(envId: string, projectId: string, mode: 'up' | 'redeploy', options?: DeployProjectOptions): Promise<Project>;
	deployProject(
		envId: string,
		projectId: string,
		mode: 'up' | 'redeploy',
		onLine: (data: unknown) => void,
		options?: DeployProjectOptions
	): Promise<Project>;
	async deployProject(
		envId: string,
		projectId: string,
		mode: 'up' | 'redeploy',
		onLineOrOptions?: ((data: unknown) => void) | DeployProjectOptions,
		maybeOptions?: DeployProjectOptions
	): Promise<Project> {
		const url = `/api/environments/${envId}/projects/${projectId}/${mode}`;
		const onLine = typeof onLineOrOptions === 'function' ? onLineOrOptions : undefined;
		const options = typeof onLineOrOptions === 'function' ? maybeOptions : onLineOrOptions;

		await this.postProjectStream(url, options ?? {}, onLine, {
			startFailed: (status) => m.progress_deploy_failed_to_start({ status }),
			streamFailed: () => m.progress_deploy_failed()
		});

		// The deploy stream doesn't return the project object; fetch fresh details.
		return this.getProjectForEnvironment(envId, projectId);
	}

	async downProject(envId: string, projectName: string): Promise<Project> {
		const project = await this.handleResponse<Omit<Project, 'environmentId'>>(
			this.api.post(`/environments/${envId}/projects/${projectName}/down`)
		);
		return { ...project, environmentId: envId };
	}

	async createProject(
		envId: string,
		projectName: string,
		composeContent: string,
		envContent?: string,
		workspaceFiles: ProjectWorkspaceFileDraft[] = [],
		tags: ProjectTag[] = []
	): Promise<Project> {
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
		const project = await this.handleResponse<Omit<Project, 'environmentId'>>(
			this.api.post(`/environments/${envId}/projects`, form)
		);
		return { ...project, environmentId: envId };
	}

	async checkUpdates(envId: string, projectId: string): Promise<ProjectUpdateInfo> {
		return this.handleResponse(
			this.api.post(`/environments/${envId}/updater/projects/${encodeURIComponent(projectId)}/check`, {})
		);
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
			environmentId,
			updateInfo: updates.updateInfo ?? compose.updateInfo ?? summary.updateInfo
		};
	}

	private async getProjectSection(path: string): Promise<Omit<Project, 'environmentId'>> {
		const response = await this.handleResponse<
			{ project?: Omit<Project, 'environmentId'>; success?: boolean } | Omit<Project, 'environmentId'>
		>(this.api.get(path, { cache: 'no-store' }));
		return 'project' in response && response.project ? response.project : (response as Omit<Project, 'environmentId'>);
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
			body: JSON.stringify(body)
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

	async getProjectStatusCountsForEnvironment(environmentId: string): Promise<ProjectStatusCounts> {
		const res = await this.api.get(`/environments/${environmentId}/projects/counts`);
		return res.data.data;
	}

	async getProjectTagsForEnvironment(environmentId: string): Promise<ProjectTagOption[]> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/projects/tags`));
	}

	async updateProjectTag(
		envId: string,
		projectId: string,
		name: string,
		attached: boolean,
		color?: ProjectTagColor
	): Promise<{ tags: ProjectTag[]; activityId?: string }> {
		return this.handleResponse(this.api.patch(`/environments/${envId}/projects/${projectId}/tags`, { name, attached, color }));
	}

	async updateProject(
		envId: string,
		projectId: string,
		name?: string,
		composeContent?: string,
		envContent?: string,
		overrideContent?: string
	): Promise<Project> {
		const payload: {
			name?: string;
			composeContent?: string;
			envContent?: string;
			overrideContent?: string;
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
		if (overrideContent !== undefined) {
			payload.overrideContent = overrideContent;
		}
		const project = await this.handleResponse<Omit<Project, 'environmentId'>>(
			this.api.put(`/environments/${envId}/projects/${projectId}`, payload)
		);
		return { ...project, environmentId: envId };
	}

	async restartProject(envId: string, projectId: string, services?: string[]): Promise<unknown> {
		let params: URLSearchParams | undefined;
		if (services && services.length > 0) {
			params = new URLSearchParams();
			for (const service of services) {
				params.append('services', service);
			}
		}
		return this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/restart`, undefined, { params }));
	}

	async archiveProject(envId: string, projectId: string): Promise<void> {
		await this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/archive`));
	}

	async unarchiveProject(envId: string, projectId: string): Promise<void> {
		await this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/unarchive`));
	}

	private async streamProjectPull(envId: string, projectId: string, onLine?: (data: any) => void): Promise<void> {
		const url = `/api/environments/${envId}/projects/${projectId}/pull`;

		const res = await fetch(url, { method: 'POST' });
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
		envId: string,
		projectId: string,
		options?: { services?: string[]; provider?: 'local' | 'depot'; push?: boolean; load?: boolean }
	): Promise<void>;
	buildProjectImages(
		envId: string,
		projectId: string,
		options: { services?: string[]; provider?: 'local' | 'depot'; push?: boolean; load?: boolean } | undefined,
		onLine: (data: any) => void
	): Promise<void>;
	async buildProjectImages(
		envId: string,
		projectId: string,
		options?: { services?: string[]; provider?: 'local' | 'depot'; push?: boolean; load?: boolean },
		onLine?: (data: any) => void
	): Promise<void> {
		const url = `/api/environments/${envId}/projects/${projectId}/build`;

		await this.postProjectStream(url, options || {}, onLine, {
			startFailed: (status) => `Failed to start project build (${status})`,
			streamFailed: () => m.build_failed()
		});
	}

	pullProjectImages(envId: string, projectId: string): Promise<void>;
	pullProjectImages(envId: string, projectId: string, onLine: (data: any) => void): Promise<void>;
	async pullProjectImages(envId: string, projectId: string, onLine?: (data: any) => void): Promise<void> {
		await this.streamProjectPull(envId, projectId, onLine);
	}

	async destroyProject(envId: string, projectName: string, removeVolumes = false): Promise<void> {
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
