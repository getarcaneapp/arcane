<script lang="ts">
	import TemplateTable from '../template-table.svelte';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { Template } from '$lib/types/template.type';

	let {
		templates = $bindable(),
		requestOptions = $bindable()
	}: {
		templates: Paginated<Template>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let selectedIds = $state<string[]>([]);
</script>

<TemplateTable bind:templates bind:selectedIds bind:requestOptions />
