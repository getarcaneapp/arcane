<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import type { Table as TableType } from '@tanstack/table-core';
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
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';
	import { truncateString } from '$lib/utils/string.utils';
	import DropdownCard from '$lib/components/dropdown-card.svelte';
	import DataTableToolbar from '$lib/components/arcane-table/arcane-table-toolbar.svelte';
	import { PersistedState } from 'runed';
	import { onMount } from 'svelte';
	import {
		EllipsisIcon,
		InspectIcon,
		FolderOpenIcon,
		GlobeIcon,
		TrashIcon,
		DownloadIcon,
		TagIcon,
		MoveToFolderIcon,
		ArrowDownIcon,
		ArrowRightIcon,
		RegistryIcon
	} from '$lib/icons';

	type TemplateTable = TableType<Template>;

	let {
		templates = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable()
	}: {
		templates: Paginated<Template>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let deletingId = $state<string | null>(null);
	let downloadingId = $state<string | null>(null);

	async function handleDeleteTemplate(id: string, name: string) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.resource_template() }),
			message: m.common_delete_confirm({ resource: `${m.resource_template()} "${name}"` }),
			confirm: {
				label: m.templates_delete_template(),
				destructive: true,
				action: async () => {
					deletingId = id;

					const result = await tryCatch(templateService.deleteTemplate(id));
					handleApiResultWithCallbacks({
						result,
						message: m.common_delete_failed({ resource: `${m.resource_template()} "${name}"` }),
						setLoadingState: (value) => (value ? null : (deletingId = null)),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_template()} "${name}"` }));
							templates = await templateService.getTemplates(requestOptions);
							deletingId = null;
						}
					});
				}
			}
		});
	}

	async function handleDownloadTemplate(id: string, name: string) {
		downloadingId = id;

		const result = await tryCatch(templateService.download(id));
		handleApiResultWithCallbacks({
			result,
			message: m.templates_download_failed(),
			setLoadingState: (value) => (value ? null : (downloadingId = null)),
			onSuccess: async () => {
				toast.success(m.templates_downloaded_success({ name }));
				templates = await templateService.getTemplates(requestOptions);
				downloadingId = null;
			}
		});
	}

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
	let collapsedGroupsState = $state<PersistedState<Record<string, boolean>> | null>(null);
	let collapsedGroups = $derived(collapsedGroupsState?.current ?? {});

	onMount(() => {
		collapsedGroupsState = new PersistedState<Record<string, boolean>>('template-groups-collapsed', {});
	});

	let groupByRegistry = $derived((customSettings.groupByRegistry as boolean) ?? false);

	function toggleGroup(groupName: string) {
		if (!collapsedGroupsState) return;
		collapsedGroupsState.current = {
			...collapsedGroupsState.current,
			[groupName]: !collapsedGroupsState.current[groupName]
		};
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
		const localName = m.templates_local_templates();
		const unknownName = m.templates_unknown_registry();

		for (const template of templates.data ?? []) {
			const registryName = getRegistryName(template);
			const group = groups.get(registryName) ?? [];
			group.push(template);
			if (!groups.has(registryName)) {
				groups.set(registryName, group);
			}
		}

		return Array.from(groups.entries()).sort(([a], [b]) => {
			if (a === localName) return -1;
			if (b === localName) return 1;
			if (a === unknownName) return 1;
			if (b === unknownName) return -1;
			return a.localeCompare(b);
		});
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
	item,
	mobileFieldVisibility
}: {
	item: Template;
	mobileFieldVisibility: MobileFieldVisibility;
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
				<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="relative size-8 p-0">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon />
				</ArcaneButton>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => goto(`/customize/templates/${item.id}`)}>
					<InspectIcon class="size-4" />
					{m.common_view_details()}
				</DropdownMenu.Item>

				<DropdownMenu.Item onclick={() => goto(`/projects/new?templateId=${item.id}`)}>
					<MoveToFolderIcon class="size-4" />
					{m.compose_create_project()}
				</DropdownMenu.Item>

				{#if item.isRemote}
					<DropdownMenu.Item onclick={() => handleDownloadTemplate(item.id, item.name)} disabled={downloadingId === item.id}>
						{#if downloadingId === item.id}
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
						disabled={deletingId === item.id}
					>
						{#if deletingId === item.id}
							<Spinner class="size-4" />
						{:else}
							<TrashIcon class="size-4" />
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
	<DropdownMenu.CheckboxItem
		bind:checked={() => groupByRegistry, (v) => (customSettings = { ...customSettings, groupByRegistry: !!v })}
	>
		{m.templates_group_by_registry()}
	</DropdownMenu.CheckboxItem>
{/snippet}

{#snippet GroupedTableView({
	table,
	renderPagination,
	mobileFieldsForOptions,
	onToggleMobileField
}: {
	table: TemplateTable;
	renderPagination: import('svelte').Snippet;
	mobileFieldsForOptions: { id: string; label: string; visible: boolean }[];
	onToggleMobileField: (fieldId: string) => void;
})}
	<div class="flex h-full flex-col">
		<div class="shrink-0 border-b">
			<DataTableToolbar
				{table}
				{selectedIds}
				selectionDisabled={true}
				mobileFields={mobileFieldsForOptions}
				{onToggleMobileField}
				customViewOptions={CustomViewOptions}
			/>
		</div>

		<div class="hidden flex-1 overflow-auto px-6 py-8 md:block">
			<div class="overflow-x-auto rounded-md border">
				<Table.Root>
					<Table.Header>
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
						{#each groupedTemplates ?? [] as [registryName, registryTemplates] (registryName)}
							<Table.Row
								class="bg-muted/50 hover:bg-muted/60 cursor-pointer transition-colors"
								onclick={() => toggleGroup(registryName)}
							>
								<Table.Cell colspan={table.getAllColumns().length} class="py-3 font-medium">
									<div class="flex items-center gap-2">
										{#if collapsedGroups[registryName]}
											<ArrowRightIcon class="text-muted-foreground size-4" />
										{:else}
											<ArrowDownIcon class="text-muted-foreground size-4" />
										{/if}
										{#if registryName === m.templates_local_templates()}
											<FolderOpenIcon class="text-muted-foreground size-4" />
										{:else}
											<RegistryIcon class="text-muted-foreground size-4" />
										{/if}
										<span>{registryName}</span>
										<span class="text-muted-foreground text-xs font-normal">({registryTemplates.length})</span>
									</div>
								</Table.Cell>
							</Table.Row>

							{#if !collapsedGroups[registryName]}
								{@const registryTemplateIds = new Set(registryTemplates.map((t) => t.id))}
								{@const registryRows = table
									.getRowModel()
									.rows.filter((row) => registryTemplateIds.has((row.original as Template).id))}

								{#each registryRows as row (row.id)}
									<Table.Row
										data-state={(selectedIds ?? []).includes((row.original as Template).id) && 'selected'}
										class="hover:bg-primary/5 transition-colors"
									>
										{#each row.getVisibleCells() as cell, i (cell.id)}
											<Table.Cell class={i === 0 ? 'pl-12' : ''}>
												<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
											</Table.Cell>
										{/each}
									</Table.Row>
								{/each}
							{/if}
						{/each}
					</Table.Body>
				</Table.Root>
			</div>
		</div>

		<div class="space-y-4 py-2 md:hidden">
			{#each groupedTemplates ?? [] as [registryName, registryTemplates] (registryName)}
				{@const registryTemplateIds = new Set(registryTemplates.map((t) => t.id))}
				{@const registryRows = table.getRowModel().rows.filter((row) => registryTemplateIds.has((row.original as Template).id))}

				<DropdownCard
					id={`template-registry-${registryName}`}
					title={registryName}
					description={`${registryTemplates.length} ${registryTemplates.length === 1 ? m.resource_template() : m.resource_templates()}`}
					icon={registryName === m.templates_local_templates() ? FolderOpenIcon : RegistryIcon}
				>
					{#each registryRows as row (row.id)}
						{@render TemplateMobileCardSnippet({ item: row.original as Template, mobileFieldVisibility })}
					{:else}
						<div class="flex h-24 items-center justify-center text-center text-muted-foreground">
							{m.common_no_results_found()}
						</div>
					{/each}
				</DropdownCard>
			{/each}
		</div>

		<div class="shrink-0 border-t px-2 py-4">
			{@render renderPagination()}
		</div>
	</div>
{/snippet}
