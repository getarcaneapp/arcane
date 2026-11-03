import type { Container } from '../container';
import type { CreateRequest, SearchPaginationSortRequest } from './pagination';

export type ContainerWritable = keyof Container;

export type ContainerCreate = CreateRequest<Container, ContainerWritable, 'name' | 'image'>;

export type ContainerListRequestOptions = SearchPaginationSortRequest & {
	groupByProject?: boolean;
};
