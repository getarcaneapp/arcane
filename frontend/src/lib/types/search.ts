import type { Category } from './category';

export interface SearchResponse {
	query: string;
	results: Category[];
	count: number;
}
