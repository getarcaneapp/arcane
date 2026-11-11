import type { DockerInfo } from '#lib/types/dockerinfo.js';
import type { PruneAllRequest, SystemDiskUsage } from '#lib/types/system.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

type ConvertedDockerRun = {
	dockerCompose: string;
	envVars: string;
	serviceName: string;
};

class SystemService extends BaseAPIService {
	async pruneAllForEnvironment(environmentId: string, options: PruneAllRequest) {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/system/prune`, options));
	}

	async startAllStoppedContainers(environmentId: string) {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/system/containers`, undefined, { params: { action: 'startstopped' } })
		);
	}

	async stopAllContainers(environmentId: string) {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/system/containers`, undefined, { params: { action: 'stopall' } })
		);
	}

	async getDockerInfoForEnvironment(environmentId: string): Promise<DockerInfo> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/system/docker/info`));
	}

	async getDiskUsage(environmentId: string, config?: APIRequestConfig): Promise<SystemDiskUsage> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/system/df`, config));
	}

	async convert(dockerRunCommand: string, environmentId: string): Promise<ConvertedDockerRun> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/system/convert`, {
				dockerRunCommand
			})
		);
	}
}

export const systemService = new SystemService();
