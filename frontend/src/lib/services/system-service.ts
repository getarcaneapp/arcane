import type { SystemPruneRequest } from '#lib/types/automation.js';
import type { DockerInfo } from '#lib/types/docker.js';
import type { SystemDiskUsage } from '#lib/types/shared.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

type ConvertedDockerRun = {
	dockerCompose: string;
	envVars: string;
	serviceName: string;
};

class SystemService extends BaseAPIService {
	async pruneAllForEnvironment(environmentId: string, options: SystemPruneRequest) {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/system/prune`, options));
	}

	async startAllStoppedContainers(environmentId: string) {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/system/containers`, undefined, { params: { action: 'startstopped' } })
		);
	}

	async stopAllContainers(environmentId: string) {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/system/containers`, undefined, { params: { action: 'stopall' } })
		);
	}

	async getDockerInfoForEnvironment(environmentId: string): Promise<DockerInfo> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/system/docker/info`));
	}

	async getDiskUsage(environmentId: string, config?: APIRequestConfig): Promise<SystemDiskUsage> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/system/df`, config));
	}

	async convert(dockerRunCommand: string, environmentId: string): Promise<ConvertedDockerRun> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/system/convert`, {
				dockerRunCommand
			})
		);
	}
}

export const systemService = new SystemService();
