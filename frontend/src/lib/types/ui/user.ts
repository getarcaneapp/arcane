import type { User } from '../user';
import type { CreateRequest, UpdateRequest } from './pagination';

export type BuiltinDashboardWidgetType =
	| 'healthy'
	| 'attentionCount'
	| 'updates'
	| 'risk'
	| 'reclaimableTotal'
	| 'environments'
	| 'attention'
	| 'activity'
	| 'jobs'
	| 'reclaimable'
	| 'memory';

export type CustomDashboardWidgetType = 'resourceQuery' | 'resource' | 'containerStats' | 'containerLogs' | 'environment';

export type DashboardWidgetType = BuiltinDashboardWidgetType | CustomDashboardWidgetType;

export type UserWritable = 'username' | 'password' | 'displayName' | 'email' | 'locale' | 'timeFormat';

export type UserCreate = CreateRequest<User, UserWritable, 'username' | 'password'>;

export type UserUpdate = UpdateRequest<User, UserWritable>;

export type ProfileWritable = 'displayName' | 'email' | 'locale' | 'timeFormat' | 'fontSize' | 'preferences';

export type ProfileUpdate = UpdateRequest<User, ProfileWritable>;
