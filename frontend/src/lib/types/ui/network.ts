import type { PaginatedWithCounts } from '../base';
import type { Network, NetworkSummary, NetworkUsageCounts } from '../network';
import type { CreateRequest } from './pagination';

export type NetworkWritable = keyof Network;

export type NetworkCreate = CreateRequest<Network, NetworkWritable, 'name'>;

export type NetworksPaginatedResponse = PaginatedWithCounts<NetworkSummary, NetworkUsageCounts>;
