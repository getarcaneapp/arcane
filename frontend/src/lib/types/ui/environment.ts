import type { Environment } from '../environment';
import type { CreateRequest, UpdateRequest } from './pagination';

export type EnvironmentWritable = 'name' | 'enabled' | 'regenerateApiKey';

export type EnvironmentCreate = CreateRequest<Environment, Exclude<EnvironmentWritable, 'regenerateApiKey'>, 'name'>;

export type EnvironmentUpdate = UpdateRequest<Environment, EnvironmentWritable>;
