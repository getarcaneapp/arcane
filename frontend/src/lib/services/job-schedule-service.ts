import type { JobScheduleConfig, JobListResponse } from '#lib/types/jobschedule.js';
import type { SchedulerRun, SchedulerRunList } from '#lib/types/scheduler.js';
import type { JobScheduleConfigUpdate } from '#lib/types/ui/jobschedule.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class JobScheduleService extends BaseAPIService {
	async getJobSchedules(environmentId: string = '0'): Promise<JobScheduleConfig> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/jobs/schedules`));
	}

	async updateJobSchedules(update: JobScheduleConfigUpdate, environmentId: string = '0'): Promise<JobScheduleConfig> {
		return this.handleResponse(this.api.patch(`/agents/${environmentId}/jobs/schedules`, update));
	}

	async listJobs(environmentId: string = '0', config?: APIRequestConfig): Promise<JobListResponse> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/jobs`, config));
	}

	async listRuns(jobId: string, environmentId: string, page = 1): Promise<SchedulerRunList> {
		return this.handleResponse(
			this.api.get(`/agents/${environmentId}/jobs/${encodeURIComponent(jobId)}/runs`, { params: { page, limit: 20 } })
		);
	}
	async getRun(jobId: string, runId: string, environmentId: string): Promise<SchedulerRun> {
		return this.handleResponse(
			this.api.get(`/agents/${environmentId}/jobs/${encodeURIComponent(jobId)}/runs/${encodeURIComponent(runId)}`)
		);
	}
	async updateRun(jobId: string, runId: string, action: 'retry' | 'cancel', environmentId: string): Promise<SchedulerRun> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/jobs/${encodeURIComponent(jobId)}/runs/${encodeURIComponent(runId)}`, undefined, {
				params: { action }
			})
		);
	}
}

export const jobScheduleService = new JobScheduleService();
