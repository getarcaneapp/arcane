import { environmentStore } from '#lib/stores/environment.store.svelte.js';

export class ResourceListPageState<TReq> {
	requestOptions = $state() as TReq;
	selectedIds = $state<string[]>([]);
	isCreateDialogOpen = $state(false);
	envId = $derived(environmentStore.selected?.id || '0');
	scope = $derived(environmentStore.shownKey);

	constructor(requestOptions: TReq) {
		this.requestOptions = requestOptions;
	}
}
