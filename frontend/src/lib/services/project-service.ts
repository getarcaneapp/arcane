import { m } from '#lib/paraglide/messages';
import { environmentStore } from '#lib/stores/environment.store.svelte';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
import type { Project, ProjectStatusCounts, ProjectTag, ProjectTagColor, ProjectTagOption } from '#lib/types/swarm';
import type { ProjectWorkspaceFileDraft } from '#lib/types/project-workspace';
import { readNdjsonStream } from '#lib/utils/streaming';
import { transformPaginationParams } from '#lib/utils/tables';
import BaseAPIService from './api-service';

export type DeployProjectOptions = {
	pullPolicy?: 'missing' | 'always' | 'never';
	forceRecreate?: boolean;
	removeOrphans?: boolean;
	recreateVolumes?: boolean;
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

		await this.postProjectStream(url, options ?? {}, onLine, {
			startFailed: (status) => m.progress_deploy_failed_to_start({ status }),
			streamFailed: () => m.progress_deploy_failed()
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
		workspaceFiles: ProjectWorkspaceFileDraft[] = [],
		tags: ProjectTag[] = []
	): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const form = new FormData();
		const uploads: File[] = [];
		const fileChanges = workspaceFiles.map((file) => {
			if (file.isDirectory) return { operation: 'create_folder' as const, relativePath: file.relativePath };
			const uploadIndex = uploads.length;
			uploads.push(new File([file.content ?? ''], file.relativePath));
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
		const response = await this.handleResponse<{ project?: Project; success?: boolean } | Project>(this.api.get(path));
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

	async getProjectStatusCounts(): Promise<ProjectStatusCounts> {
		const envId = await this.resolveEnvironmentId();
		return this.getProjectStatusCountsForEnvironment(envId);
	}

	async getProjectStatusCountsForEnvironment(environmentId: string): Promise<ProjectStatusCounts> {
		const res = await this.api.get(`/environments/${environmentId}/projects/counts`);
		return res.data.data;
	}

	async getProjectTagsForEnvironment(environmentId: string): Promise<ProjectTagOption[]> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/projects/tags`));
	}

	async updateProjectTag(
		projectId: string,
		name: string,
		attached: boolean,
		color?: ProjectTagColor
	): Promise<{ tags: ProjectTag[]; activityId?: string }> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.patch(`/environments/${envId}/projects/${projectId}/tags`, { name, attached, color }));
	}

	async updateProject(
		projectId: string,
		name?: string,
		composeContent?: string,
		envContent?: string,
		overrideContent?: string
	): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
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
		return this.handleResponse(this.api.put(`/environments/${envId}/projects/${projectId}`, payload));
	}

	async restartProject(projectId: string, services?: string[]): Promise<unknown> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		let params: URLSearchParams | undefined;
		if (services && services.length > 0) {
			params = new URLSearchParams();
			for (const service of services) {
				params.append('services', service);
			}
		}
		return this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/restart`, undefined, { params }));
	}

	async archiveProject(projectId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/archive`));
	}

	async unarchiveProject(projectId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.post(`/environments/${envId}/projects/${projectId}/unarchive`));
	}

	redeployProject(projectId: string, options?: DeployProjectOptions): Promise<Project>;
	redeployProject(projectId: string, onLine: (data: any) => void, options?: DeployProjectOptions): Promise<Project>;
	async redeployProject(
		projectId: string,
		onLineOrOptions?: ((data: any) => void) | DeployProjectOptions,
		maybeOptions?: DeployProjectOptions
	): Promise<Project> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const url = `/api/environments/${envId}/projects/${projectId}/redeploy`;
		const onLine = typeof onLineOrOptions === 'function' ? onLineOrOptions : undefined;
		const options = typeof onLineOrOptions === 'function' ? maybeOptions : onLineOrOptions;

		// Redeploy always pre-pulls server-side; pullPolicy only governs the up phase.
		await this.postProjectStream(url, options ?? {}, onLine, {
			startFailed: (status) => m.progress_deploy_failed_to_start({ status }),
			streamFailed: () => m.progress_deploy_failed()
		});

		// The redeploy stream doesn't return the project object; fetch fresh details.
		return this.getProject(projectId);
	}

	private async streamProjectPull(projectId: string, onLine?: (data: any) => void): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
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

		await this.postProjectStream(url, options || {}, onLine, {
			startFailed: (status) => `Failed to start project build (${status})`,
			streamFailed: () => m.build_failed()
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
