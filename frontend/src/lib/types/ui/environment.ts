import type { Environment } from '../environment';
import type { CreateRequest, UpdateRequest } from './pagination';

export type EnvironmentWritable = 'name' | 'enabled';

export type EnvironmentCreate = CreateRequest<Environment, EnvironmentWritable, 'name'>;

export type EnvironmentUpdate = UpdateRequest<Environment, EnvironmentWritable>;
