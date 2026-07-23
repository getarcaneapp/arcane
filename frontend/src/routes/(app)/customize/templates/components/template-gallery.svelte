<script lang="ts">
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import ArcaneTablePagination from '#lib/components/arcane-table/arcane-table-pagination.svelte';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import TemplateCard from './template-card.svelte';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '#lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks, tryCatch } from '#lib/utils/api';
	import { templateService } from '#lib/services/template-service';
	import { templateTypeFilters } from '#lib/components/arcane-table/data';
	import { debounced } from '#lib/utils/ws';
	import { hasPermission } from '#lib/utils/auth';
	import { m } from '#lib/paraglide/messages';
	import { untrack } from 'svelte';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
	import type { Template } from '#lib/types/swarm';
	import { SearchIcon, TemplateIcon } from '#lib/icons';
	import { tablePreferences } from '#lib/stores/table-preferences.store.svelte';

	let {
		templates = $bindable(),
		requestOptions = $bindable()
	}: {
		templates: Paginated<Template>;
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let deletingId = $state<string | null>(null);
	let downloadingId = $state<string | null>(null);
	let searchValue = $state(untrack(() => requestOptions.search ?? ''));
	let typeFilter = $state(untrack(() => (requestOptions.filters?.['type'] as string) ?? 'all'));

	const currentPage = $derived(templates.pagination?.currentPage ?? requestOptions.pagination?.page ?? 1);
	const totalPages = $derived(templates.pagination?.totalPages ?? 1);
	const totalItems = $derived(templates.pagination?.totalItems ?? 0);
	const pageSize = $derived(requestOptions.pagination?.limit ?? templates.pagination?.itemsPerPage ?? 20);
	const hasActiveQuery = $derived(!!requestOptions.search || requestOptions.filters?.['type'] !== undefined);
	const canCreateTemplate = $derived(hasPermission('templates:create'));

	const typeFilterLabel = $derived(templateTypeFilters.find((f) => f.value === typeFilter)?.label ?? m.common_all());

	async function refetch() {
		templates = await templateService.getTemplates(requestOptions);
	}

	function applySearch(value: string) {
		const search = value.trim();
		requestOptions = {
			...requestOptions,
			search: search || undefined,
			pagination: { page: 1, limit: pageSize }
		};
		tablePreferences.update('arcane-template-gallery', { g: search });
		refetch();
	}

	const debouncedSearch = debounced(applySearch, 300);

	function applyTypeFilter(value: string) {
		typeFilter = value;
		const filters = { ...requestOptions.filters };
		if (value === 'all') {
			delete filters['type'];
		} else {
			filters['type'] = value;
		}
		requestOptions = {
			...requestOptions,
			filters,
			pagination: { page: 1, limit: pageSize }
		};
		tablePreferences.update('arcane-template-gallery', { f: value === 'all' ? [] : [['type', value]] });
		refetch();
	}

	function setPage(page: number) {
		if (page < 1) page = 1;
		if (totalPages > 0 && page > totalPages) page = totalPages;
		requestOptions = { ...requestOptions, pagination: { page, limit: pageSize } };
		refetch();
	}

	function setPageSize(limit: number) {
		tablePreferences.update('arcane-template-gallery', { l: limit });
		requestOptions = { ...requestOptions, pagination: { page: 1, limit } };
		refetch();
	}

	async function handleDeleteTemplate(template: Template) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.resource_template() }),
			message: m.common_delete_confirm({ resource: `${m.resource_template()} "${template.name}"` }),
			confirm: {
				label: m.templates_delete_template(),
				destructive: true,
				action: async () => {
					deletingId = template.id;

					const result = await tryCatch(templateService.deleteTemplate(template.id));
					handleApiResultWithCallbacks({
						result,
						message: m.common_delete_failed({ resource: `${m.resource_template()} "${template.name}"` }),
						setLoadingState: (value) => (value ? null : (deletingId = null)),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_template()} "${template.name}"` }));
							await refetch();
							deletingId = null;
						}
					});
				}
			}
		});
	}

	async function handleDownloadTemplate(template: Template) {
		downloadingId = template.id;

		const result = await tryCatch(templateService.download(template.id));
		handleApiResultWithCallbacks({
			result,
			message: m.templates_download_failed(),
			setLoadingState: (value) => (value ? null : (downloadingId = null)),
			onSuccess: async () => {
				toast.success(m.templates_downloaded_success({ name: template.name }));
				await refetch();
				downloadingId = null;
			}
		});
	}
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-center gap-2">
		<div class="relative min-w-0 flex-1 md:w-64 md:flex-none">
			<SearchIcon class="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
			<Input
				placeholder={m.templates_search_placeholder()}
				bind:value={searchValue}
				oninput={(e) => debouncedSearch(e.currentTarget.value)}
				onchange={(e) => applySearch(e.currentTarget.value)}
				onkeydown={(e) => {
					if (e.key === 'Enter') applySearch((e.currentTarget as HTMLInputElement).value);
				}}
				class="h-9 w-full pl-8"
			/>
		</div>

		<Select.Root allowDeselect={false} type="single" value={typeFilter} onValueChange={applyTypeFilter}>
			<Select.Trigger class="h-9 w-[140px]">
				{typeFilterLabel}
			</Select.Trigger>
			<Select.Content>
				<Select.Item value="all">{m.common_all()}</Select.Item>
				{#each templateTypeFilters as option (String(option.value))}
					<Select.Item value={String(option.value)}>{option.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
	</div>

	{#if templates.data.length > 0}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
			{#each templates.data as template (template.id)}
				<TemplateCard
					{template}
					downloading={downloadingId === template.id}
					deleting={deletingId === template.id}
					onDownload={handleDownloadTemplate}
					onDelete={handleDeleteTemplate}
				/>
			{/each}
		</div>

		<ArcaneTablePagination
			items={templates}
			{currentPage}
			{totalPages}
			{totalItems}
			{pageSize}
			canPrev={currentPage > 1}
			canNext={currentPage < totalPages}
			{setPage}
			{setPageSize}
		/>
	{:else if hasActiveQuery}
		<EmptyState
			icon={SearchIcon}
			title={m.common_no_results_found()}
			description={m.common_no_results_hint()}
			class="rounded-xl border border-border/50 py-12"
		/>
	{:else}
		<EmptyState
			icon={TemplateIcon}
			title={m.templates_no_templates()}
			actionLabel={canCreateTemplate ? m.templates_create_template() : undefined}
			actionHref={canCreateTemplate ? '/customize/templates/create' : undefined}
			class="rounded-xl border border-border/50 py-12"
		/>
	{/if}
</div>
