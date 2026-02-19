import BaseAPIService from './api-service';
import type { JobSchedules, JobSchedulesUpdate, JobListResponse, JobRunResponse } from '$lib/types/job-schedule.type';

class JobScheduleService extends BaseAPIService {
	async getJobSchedules(environmentId: string = '0'): Promise<JobSchedules> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/job-schedules`));
	}

	async updateJobSchedules(update: JobSchedulesUpdate, environmentId: string = '0'): Promise<JobSchedules> {
		return this.handleResponse(this.api.patch(`/environments/${environmentId}/job-schedules`, update));
	}

	async listJobs(environmentId: string = '0'): Promise<JobListResponse> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/jobs`));
	}

	async runJob(jobId: string, environmentId: string = '0'): Promise<JobRunResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/jobs/${jobId}/run`));
	}
}

export const jobScheduleService = new JobScheduleService();
export default JobScheduleService;
