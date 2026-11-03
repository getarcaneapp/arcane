import type { Template, TemplateRegistry } from '../template';
import type { CreateRequest, UpdateRequest } from './pagination';

export type TemplateRegistryWritable = 'name' | 'description' | 'url' | 'enabled';

export type TemplateRegistryCreate = CreateRequest<TemplateRegistry, TemplateRegistryWritable, 'name' | 'url'>;

export type TemplateRegistryUpdate = UpdateRequest<TemplateRegistry, TemplateRegistryWritable>;

export type TemplateWritable = 'name' | 'description' | 'content' | 'envContent';

export type TemplateCreate = CreateRequest<Template, TemplateWritable, 'name' | 'content'>;

export type TemplateUpdate = UpdateRequest<Template, TemplateWritable>;
