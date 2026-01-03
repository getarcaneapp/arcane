import BaseAPIService from './api-service';
import type { JobSchedules, JobSchedulesUpdate } from '$lib/types/job-schedule.type';

class JobScheduleService extends BaseAPIService {
	async getJobSchedules(): Promise<JobSchedules> {
		return this.handleResponse(this.api.get('/job-schedules'));
	}

	async updateJobSchedules(update: JobSchedulesUpdate): Promise<JobSchedules> {
		return this.handleResponse(this.api.put('/job-schedules', update));
	}
}

export const jobScheduleService = new JobScheduleService();
export default JobScheduleService;
