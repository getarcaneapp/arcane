import type { JobRun, JobRunList } from '#lib/types/job.js';
import BaseAPIService from './api-service';
import type { JobSchedules, JobSchedulesUpdate, JobListResponse, JobRunResponse } from '#lib/types/settings.js';

class JobScheduleService extends BaseAPIService {
	async getJobSchedules(environmentId: string = '0'): Promise<JobSchedules> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/job-schedules`));
	}

	async updateJobSchedules(update: JobSchedulesUpdate, environmentId: string = '0'): Promise<JobSchedules> {
		return this.handleResponse(this.api.put(`/environments/${environmentId}/job-schedules`, update));
	}

	async listJobs(environmentId: string = '0'): Promise<JobListResponse> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/jobs`));
	}

	async runJob(jobId: string, environmentId: string = '0', requestId: string = crypto.randomUUID()): Promise<JobRunResponse> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/jobs/${encodeURIComponent(jobId)}/run`, undefined, {
				headers: { 'Idempotency-Key': requestId }
			})
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
				`/environments/${environmentId}/jobs/${encodeURIComponent(jobId)}/runs/${encodeURIComponent(runId)}/${action}`
			)
		);
	}
	async restartWorker(jobId: string, environmentId: string): Promise<{ success: boolean; message: string }> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/jobs/${encodeURIComponent(jobId)}/restart`));
	}
}

export const jobScheduleService = new JobScheduleService();
