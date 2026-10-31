import type { JobRun, JobRunList } from '#lib/types/job.js';
import type { JobScheduleConfig, JobScheduleConfigUpdate, JobListResponse } from '#lib/types/settings.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class JobScheduleService extends BaseAPIService {
	async getJobSchedules(environmentId: string = '0'): Promise<JobScheduleConfig> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/jobs/schedules`));
	}

	async updateJobSchedules(update: JobScheduleConfigUpdate, environmentId: string = '0'): Promise<JobScheduleConfig> {
		return this.handleResponse(this.api.patch(`/environments/${environmentId}/jobs/schedules`, update));
	}

	async listJobs(environmentId: string = '0', config?: APIRequestConfig): Promise<JobListResponse> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/jobs`, config));
	}

	async listRuns(jobId: string, environmentId: string, page = 1): Promise<JobRunList> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/jobs/${encodeURIComponent(jobId)}/runs`, { params: { page, limit: 20 } })
		);
	}
	async getRun(jobId: string, runId: string, environmentId: string): Promise<JobRun> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/jobs/${encodeURIComponent(jobId)}/runs/${encodeURIComponent(runId)}`)
		);
	}
	async updateRun(jobId: string, runId: string, action: 'retry' | 'cancel', environmentId: string): Promise<JobRun> {
		return this.handleResponse(
			this.api.post(
				`/environments/${environmentId}/jobs/${encodeURIComponent(jobId)}/runs/${encodeURIComponent(runId)}`,
				undefined,
				{
					params: { action }
				}
			)
		);
	}
}

export const jobScheduleService = new JobScheduleService();
