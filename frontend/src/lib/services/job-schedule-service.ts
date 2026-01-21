import BaseAPIService from './api-service';
import type { JobSchedules, JobSchedulesUpdate, JobListResponse, JobRunResponse } from '$lib/types/job-schedule.type';

class JobScheduleService extends BaseAPIService {
	async getJobSchedules(): Promise<JobSchedules> {
		return this.handleResponse(this.api.get('/job-schedules'));
	}

	async updateJobSchedules(update: JobSchedulesUpdate): Promise<JobSchedules> {
		return this.handleResponse(this.api.put('/job-schedules', update));
	}

	async listJobs(): Promise<JobListResponse> {
		return this.handleResponse(this.api.get('/jobs'));
	}

	async runJob(jobId: string): Promise<JobRunResponse> {
		return this.handleResponse(this.api.post(`/jobs/${jobId}/run`));
	}
}

export const jobScheduleService = new JobScheduleService();
export default JobScheduleService;
