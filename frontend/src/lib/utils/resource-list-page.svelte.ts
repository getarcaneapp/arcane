import type { CreateQueryOptions, QueryKey } from '@tanstack/svelte-query';

import { environmentStore } from '#lib/stores/environment.store.svelte.js';

export class ResourceListPageState<TReq> {
	requestOptions = $state() as TReq;
	selectedIds = $state<string[]>([]);
	isCreateDialogOpen = $state(false);
	scope = $derived(environmentStore.shownKey);

	constructor(requestOptions: TReq) {
		this.requestOptions = requestOptions;
	}
}

export function scopedQueryOptions<TQueryFnData, TError = Error, TQueryKey extends QueryKey = QueryKey>(
	scope: string,
	options: Omit<CreateQueryOptions<TQueryFnData, TError, TQueryFnData, TQueryKey>, 'placeholderData' | 'select'>
) {
	return {
		...options,
		placeholderData: (previous: TQueryFnData | undefined, query?: { queryKey: readonly unknown[] }) =>
			query?.queryKey[1] === scope ? previous : undefined,
		select: (value: TQueryFnData) => ({ envId: scope, value })
	};
}
