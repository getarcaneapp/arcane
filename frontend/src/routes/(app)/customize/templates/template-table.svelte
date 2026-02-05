<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
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
	import { PersistedState } from 'runed';
	import { onMount } from 'svelte';
	import {
		InspectIcon,
		FolderOpenIcon,
		GlobeIcon,
		TrashIcon,
		DownloadIcon,
		TagIcon,
		MoveToFolderIcon,
		RegistryIcon,
		EllipsisIcon
	} from '$lib/icons';

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

	// Group by function for templates
	function groupTemplateByRegistry(template: Template): string {
		return getRegistryName(template);
	}

	// Icon for each group
	function getGroupIcon(groupName: string) {
		if (groupName === m.templates_local_templates()) {
			return FolderOpenIcon;
		}
		return RegistryIcon;
	}
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
				<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-8">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon class="size-4" />
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

				<DropdownMenu.Separator />

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
	groupBy={groupByRegistry ? groupTemplateByRegistry : undefined}
	groupIcon={groupByRegistry ? getGroupIcon : undefined}
	groupCollapsedState={collapsedGroups}
	onGroupToggle={toggleGroup}
/>

{#snippet CustomViewOptions()}
	<DropdownMenu.CheckboxItem
		bind:checked={() => groupByRegistry, (v) => (customSettings = { ...customSettings, groupByRegistry: !!v })}
	>
		{m.templates_group_by_registry()}
	</DropdownMenu.CheckboxItem>
{/snippet}
