<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import ScanSearchIcon from '@lucide/svelte/icons/scan-search';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import PlusCircleIcon from '@lucide/svelte/icons/plus-circle';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import type { Table as TableType, Row } from '@tanstack/table-core';
	import * as Table from '$lib/components/ui/table/index.js';
	import FlexRender from '$lib/components/ui/data-table/flex-render.svelte';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import UniversalMobileCard from '$lib/components/arcane-table/cards/universal-mobile-card.svelte';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { Template } from '$lib/types/template.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';
	import TagIcon from '@lucide/svelte/icons/tag';
	import { truncateString } from '$lib/utils/string.utils';
	import DropdownCard from '$lib/components/dropdown-card.svelte';
	import { DataTableViewOptions } from '$lib/components/arcane-table/index.js';

	let {
		templates = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable()
	}: {
		templates: Paginated<Template>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let isLoading = $state({
		deleting: false,
		downloading: false
	});

	async function handleDeleteTemplate(id: string, name: string) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.resource_template() }),
			message: m.common_delete_confirm({ resource: `${m.resource_template()} "${name}"` }),
			confirm: {
				label: m.templates_delete_template(),
				destructive: true,
				action: async () => {
					isLoading.deleting = true;

					const result = await tryCatch(templateService.deleteTemplate(id));
					handleApiResultWithCallbacks({
						result,
						message: m.common_delete_failed({ resource: `${m.resource_template()} "${name}"` }),
						setLoadingState: (value) => (isLoading.deleting = value),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_template()} "${name}"` }));
							templates = await templateService.getTemplates(requestOptions);
						}
					});
				}
			}
		});
	}

	async function handleDownloadTemplate(id: string, name: string) {
		isLoading.downloading = true;

		const result = await tryCatch(templateService.download(id));
		handleApiResultWithCallbacks({
			result,
			message: m.templates_download_failed(),
			setLoadingState: (value) => (isLoading.downloading = value),
			onSuccess: async () => {
				toast.success(m.templates_downloaded_success({ name }));
				templates = await templateService.getTemplates(requestOptions);
			}
		});
	}

	const isAnyLoading = $derived(Object.values(isLoading).some((loading) => loading));

	const columns = [
		{
			accessorKey: 'name',
			title: m.common_name(),
			sortable: true,
			cell: NameCell
		},
		{
			accessorKey: 'description',
			title: m.common_description(),
			cell: DescriptionCell
		},
		{
			id: 'type',
			accessorFn: (row) => row.isRemote,
			title: m.common_type(),
			sortable: true,
			cell: TypeCell
		},
		{
			accessorKey: 'metadata',
			title: m.common_tags(),
			cell: TagsCell
		}
	] satisfies ColumnSpec<Template>[];

	const mobileFields = [
		{ id: 'description', label: m.common_description(), defaultVisible: true },
		{ id: 'type', label: m.common_type(), defaultVisible: true },
		{ id: 'tags', label: m.common_tags(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let customSettings = $state<Record<string, unknown>>({});

	let groupByRegistry = $derived.by(() => {
		return (customSettings.groupByRegistry as boolean) ?? false;
	});

	function setGroupByRegistry(value: boolean) {
		customSettings = { ...customSettings, groupByRegistry: value };
	}

	function getRegistryName(template: Template): string {
		if (template.registry?.name) {
			return template.registry.name;
		}
		if (template.isRemote) {
			return m.templates_unknown_registry();
		}
		return m.templates_local_templates();
	}

	const groupedTemplates = $derived.by(() => {
		if (!groupByRegistry) return null;

		const groups = new Map<string, Template[]>();

		for (const template of templates.data ?? []) {
			const registryName = getRegistryName(template);
			if (!groups.has(registryName)) {
				groups.set(registryName, []);
			}
			const group = groups.get(registryName);
			if (group) {
				group.push(template);
			}
		}
		const sortedGroups = Array.from(groups.entries()).sort(([a], [b]) => {
			if (a === m.templates_local_templates()) return -1;
			if (b === m.templates_local_templates()) return 1;
			if (a === m.templates_unknown_registry()) return 1;
			if (b === m.templates_unknown_registry()) return -1;
			return a.localeCompare(b);
		});

		return sortedGroups;
	});
</script>

{#snippet NameCell({ item }: { item: Template })}
	<a class="font-medium hover:underline" href="/customize/templates/{item.id}">
		{item.name}
	</a>
{/snippet}

{#snippet DescriptionCell({ item }: { item: Template })}
	<span class="text-muted-foreground line-clamp-2 text-sm">
		{truncateString(item.description, 80)}
	</span>
{/snippet}

{#snippet TypeCell({ item }: { item: Template })}
	{#if item.isRemote}
		<Badge variant="secondary" class="gap-1">
			<GlobeIcon class="size-3" />
			{m.templates_remote()}
		</Badge>
	{:else}
		<Badge variant="secondary" class="gap-1">
			<FolderOpenIcon class="size-3" />
			{m.templates_local()}
		</Badge>
	{/if}
{/snippet}

{#snippet TagsCell({ item }: { item: Template })}
	{#if item.metadata?.tags && item.metadata.tags.length > 0}
		<div class="flex flex-wrap gap-1">
			{#each item.metadata.tags.slice(0, 2) as tag}
				<Badge variant="outline" class="text-xs">{tag}</Badge>
			{/each}
			{#if item.metadata.tags.length > 2}
				<Badge variant="outline" class="text-xs">+{item.metadata.tags.length - 2}</Badge>
			{/if}
		</div>
	{/if}
{/snippet}

{#snippet TemplateMobileCardSnippet({
	row,
	item,
	mobileFieldVisibility
}: {
	row: Row<Template>;
	item: Template;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={(item) => ({
			component: item.isRemote ? GlobeIcon : FolderOpenIcon,
			variant: item.isRemote ? 'emerald' : 'blue'
		})}
		title={(item) => item.name}
		subtitle={(item) => ((mobileFieldVisibility.description ?? true) ? item.description : null)}
		badges={[
			(item) =>
				(mobileFieldVisibility.type ?? true)
					? {
							variant: item.isRemote ? 'green' : 'blue',
							text: item.isRemote ? m.templates_remote() : m.templates_local()
						}
					: null
		]}
		fields={[]}
		rowActions={RowActions}
		onclick={(item: Template) => goto(`/customize/templates/${item.id}`)}
	>
		{#snippet children()}
			{#if (mobileFieldVisibility.tags ?? true) && item.metadata?.tags && item.metadata.tags.length > 0}
				<div class="flex items-start gap-2.5 border-t pt-3">
					<div class="flex size-7 shrink-0 items-center justify-center rounded-lg bg-purple-500/10">
						<TagIcon class="size-3.5 text-purple-500" />
					</div>
					<div class="min-w-0 flex-1">
						<div class="text-muted-foreground text-[10px] font-medium tracking-wide uppercase">
							{m.common_tags()}
						</div>
						<div class="mt-1 flex flex-wrap gap-1">
							{#each item.metadata.tags.slice(0, 3) as tag}
								<Badge variant="outline" class="text-xs">{tag}</Badge>
							{/each}
							{#if item.metadata.tags.length > 3}
								<Badge variant="outline" class="text-xs">+{item.metadata.tags.length - 3}</Badge>
							{/if}
						</div>
					</div>
				</div>
			{/if}
		{/snippet}
	</UniversalMobileCard>
{/snippet}

{#snippet RowActions({ item }: { item: Template })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="ghost" size="icon" class="relative size-8 p-0">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon />
				</Button>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => goto(`/customize/templates/${item.id}`)} disabled={isAnyLoading}>
					<ScanSearchIcon class="size-4" />
					{m.common_view_details()}
				</DropdownMenu.Item>

				<DropdownMenu.Item onclick={() => goto(`/projects/new?templateId=${item.id}`)} disabled={isAnyLoading}>
					<PlusCircleIcon class="size-4" />
					{m.compose_create_project()}
				</DropdownMenu.Item>

				{#if item.isRemote}
					<DropdownMenu.Item
						onclick={() => handleDownloadTemplate(item.id, item.name)}
						disabled={isLoading.downloading || isAnyLoading}
					>
						{#if isLoading.downloading}
							<Spinner class="size-4" />
						{:else}
							<DownloadIcon class="size-4" />
						{/if}
						{m.templates_download()}
					</DropdownMenu.Item>
				{:else}
					<DropdownMenu.Separator />
					<DropdownMenu.Item
						variant="destructive"
						onclick={() => handleDeleteTemplate(item.id, item.name)}
						disabled={isLoading.deleting || isAnyLoading}
					>
						{#if isLoading.deleting}
							<Spinner class="size-4" />
						{:else}
							<Trash2Icon class="size-4" />
						{/if}
						{m.templates_delete_template()}
					</DropdownMenu.Item>
				{/if}
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-template-table"
	items={templates}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	bind:customSettings
	onRefresh={async (options) => (templates = await templateService.getTemplates(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={TemplateMobileCardSnippet}
	selectionDisabled
	customViewOptions={CustomViewOptions}
	customTableView={groupByRegistry && groupedTemplates ? GroupedTableView : undefined}
/>

{#snippet CustomViewOptions()}
	<DropdownMenu.CheckboxItem bind:checked={() => groupByRegistry, (v) => setGroupByRegistry(!!v)}>
		{m.templates_group_by_registry()}
	</DropdownMenu.CheckboxItem>
{/snippet}

{#snippet GroupedTableView({ table }: { table: TableType<Template> })}
	<div class="mb-4 flex items-center justify-end border-b px-6 py-4">
		<DataTableViewOptions {table} customViewOptions={CustomViewOptions} />
	</div>
	<div class="space-y-4 px-6 pb-6">
		{#each groupedTemplates ?? [] as [registryName, registryTemplates] (registryName)}
			{@const registryTemplateIds = new Set(registryTemplates.map((t) => t.id))}
			{@const registryRows = table.getRowModel().rows.filter((row) => registryTemplateIds.has(row.original.id))}

			<DropdownCard
				id={`template-registry-${registryName}`}
				title={registryName}
				description={`${registryTemplates.length} ${registryTemplates.length === 1 ? m.resource_template() : m.resource_templates()}`}
				icon={registryName === m.templates_local_templates() ? FolderOpenIcon : GlobeIcon}
			>
				<div class="hidden md:block">
					<Table.Root
						class="**:data-[slot='table-container']:rounded-none **:data-[slot='table-container']:border-0 **:data-[slot='table-container']:bg-transparent **:data-[slot='table-container']:shadow-none **:data-[slot='table-container']:backdrop-filter-none"
					>
						<Table.Header class="border-border/40 border-t">
							{#each table.getHeaderGroups() as headerGroup (headerGroup.id)}
								<Table.Row>
									{#each headerGroup.headers as header (header.id)}
										<Table.Head colspan={header.colSpan}>
											{#if !header.isPlaceholder}
												<FlexRender content={header.column.columnDef.header} context={header.getContext()} />
											{/if}
										</Table.Head>
									{/each}
								</Table.Row>
							{/each}
						</Table.Header>
						<Table.Body>
							{#each registryRows as row (row.id)}
								<Table.Row data-state={(selectedIds ?? []).includes((row.original as Template).id) && 'selected'}>
									{#each row.getVisibleCells() as cell (cell.id)}
										<Table.Cell>
											<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
										</Table.Cell>
									{/each}
								</Table.Row>
							{:else}
								<Table.Row>
									<Table.Cell colspan={table.getAllColumns().length} class="h-24 text-center"
										>{m.common_no_results_found()}</Table.Cell
									>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>

				<div class="space-y-3 md:hidden">
					{#each registryRows as row (row.id)}
						{@render TemplateMobileCardSnippet({ row, item: row.original as Template, mobileFieldVisibility })}
					{:else}
						<div class="flex h-24 items-center justify-center text-center text-muted-foreground">
							{m.common_no_results_found()}
						</div>
					{/each}
				</div>
			</DropdownCard>
		{/each}
	</div>
{/snippet}
