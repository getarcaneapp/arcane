import type { JobScheduleConfig } from '../jobschedule';
import type { UpdateRequest } from './pagination';

export type JobScheduleConfigUpdate = UpdateRequest<JobScheduleConfig, keyof JobScheduleConfig>;
