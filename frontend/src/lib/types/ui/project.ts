import type { Project } from '../project';
import type { UpdateRequest } from './pagination';

export type ProjectWritable = 'name' | 'composeContent' | 'envContent' | 'overrideContent';

export type ProjectUpdate = UpdateRequest<Project, ProjectWritable>;
