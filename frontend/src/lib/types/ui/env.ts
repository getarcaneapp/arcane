import type { GlobalVariable } from '../env';
import type { CreateRequest, UpdateRequest } from './pagination';

export type GlobalVariableWritable = 'key' | 'value' | 'isSecret' | 'allEnvironments' | 'environmentIds';

export type GlobalVariableCreate = CreateRequest<GlobalVariable, GlobalVariableWritable, 'key' | 'value'>;

export type GlobalVariableUpdate = UpdateRequest<GlobalVariable, GlobalVariableWritable>;
