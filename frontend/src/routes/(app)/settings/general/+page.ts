import type { PageLoad } from './$types';
import { jobScheduleService } from '$lib/services/job-schedule-service';

export const load: PageLoad = async () => {
	try {
		const jobSchedules = await jobScheduleService.getJobSchedules();
		return { jobSchedules };
	} catch (error) {
		console.error('Failed to load job schedules:', error);
		throw error;
	}
};
