import { environmentStore } from '$lib/stores/environment.store.svelte';

export class ResourceListPageState<TItems, TReq> {
	items = $state() as TItems;
	requestOptions = $state() as TReq;
	selectedIds = $state<string[]>([]);
	isCreateDialogOpen = $state(false);
	envId = $derived(environmentStore.selected?.id || '0');

	constructor(items: TItems, requestOptions: TReq) {
		this.items = items;
		this.requestOptions = requestOptions;
	}
}
