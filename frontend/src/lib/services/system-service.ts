import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { DockerInfo } from '$lib/types/docker';
import type { SystemPruneRequest } from '$lib/types/automation';

type ConvertedDockerRun = {
	dockerCompose: string;
	envVars: string;
	serviceName: string;
};

class SystemService extends BaseAPIService {
	async pruneAll(options: SystemPruneRequest) {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.pruneAllForEnvironment(envId, options);
	}

	async pruneAllForEnvironment(environmentId: string, options: SystemPruneRequest) {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/system/prune`, options));
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

	async convert(dockerRunCommand: string): Promise<ConvertedDockerRun> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.post(`/environments/${envId}/system/convert`, {
				dockerRunCommand
			})
		);
	}
}

export const systemService = new SystemService();
