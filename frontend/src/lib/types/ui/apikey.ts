import type { ApiKey } from '../apikey';
import type { CreateRequest, UpdateRequest } from './pagination';

export type ApiKeyWritable = 'name' | 'description' | 'expiresAt' | 'permissions';

export type ApiKeyCreate = CreateRequest<ApiKey, ApiKeyWritable, 'name' | 'permissions'>;

export type PersonalApiKeyCreate = CreateRequest<ApiKey, Exclude<ApiKeyWritable, 'permissions'>, 'name'>;

export type ApiKeyUpdate = UpdateRequest<ApiKey, ApiKeyWritable>;
