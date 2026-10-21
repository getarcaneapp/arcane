import type { JobRun, JobRunList } from '#lib/types/job.js';
import type { JobScheduleConfig, JobScheduleConfigUpdate, JobListResponse, JobRunResponse } from '#lib/types/settings.js';

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

	async runJob(jobId: string, environmentId: string = '0'): Promise<JobRunResponse> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/jobs/${encodeURIComponent(jobId)}`, undefined, { params: { action: 'run' } })
		);
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
	async restartWorker(jobId: string, environmentId: string): Promise<{ success: boolean; message: string }> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/jobs/${encodeURIComponent(jobId)}`, undefined, {
				params: { action: 'restart' }
			})
		);
	}
}

export const jobScheduleService = new JobScheduleService();
