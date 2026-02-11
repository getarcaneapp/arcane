import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { DockerInfo } from '$lib/types/docker-info.type';

export class SystemService extends BaseAPIService {
	async pruneAll(options: {
		containers?: boolean;
		images?: boolean;
		volumes?: boolean;
		networks?: boolean;
		buildCache?: boolean;
		dangling?: boolean;
		until?: string;
	}) {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/system/prune`, options));
	}

	async startAllStoppedContainers() {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/system/containers/start-stopped`));
	}

	async stopAllContainers() {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/system/containers/stop-all`));
	}

	async getDockerInfo(): Promise<DockerInfo> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.getDockerInfoForEnvironment(envId);
	}

	async getDockerInfoForEnvironment(environmentId: string): Promise<DockerInfo> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/system/docker/info`));
	}

	async convert(dockerRunCommand: string) {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.post(`/environments/${envId}/system/convert`, {
			dockerRunCommand
		});
		return res.data;
	}
}

export const systemService = new SystemService();
