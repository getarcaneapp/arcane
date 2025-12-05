<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import ScanSearchIcon from '@lucide/svelte/icons/scan-search';
	import PlayIcon from '@lucide/svelte/icons/play';
	import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';
	import StopCircleIcon from '@lucide/svelte/icons/stop-circle';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import ArrowUpCircleIcon from '@lucide/svelte/icons/arrow-up-circle';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { format } from 'date-fns';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import type { ContainerSummaryDto } from '$lib/types/container.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { PortBadge } from '$lib/components/badges/index.js';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import BoxIcon from '@lucide/svelte/icons/box';
	import ImageIcon from '@lucide/svelte/icons/image';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import ClockIcon from '@lucide/svelte/icons/clock';
	import { containerService } from '$lib/services/container-service';
	import DropdownCard from '$lib/components/dropdown-card.svelte';
	import FolderIcon from '@lucide/svelte/icons/folder';
	import type { Table as TableType } from '@tanstack/table-core';
	import * as Table from '$lib/components/ui/table/index.js';
	import FlexRender from '$lib/components/ui/data-table/flex-render.svelte';
	import { DataTableViewOptions } from '$lib/components/arcane-table/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import ImageUpdateItem from '$lib/components/image-update-item.svelte';

	type FieldVisibility = Record<string, boolean>;

	let {
		containers = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		baseServerUrl = $bindable()
	}: {
		containers: Paginated<ContainerSummaryDto>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		baseServerUrl: string;
	} = $props();

	let isLoading = $state({
		start: false,
		stop: false,
		restart: false,
		remove: false,
		updating: false
	});

	// Parse image reference into repo and tag
	function parseImageRef(imageRef: string): { repo: string; tag: string } {
		// Handle images like "nginx:latest", "library/nginx:1.0", "ghcr.io/org/image:tag"
		const lastColon = imageRef.lastIndexOf(':');
		// Check if colon is part of a tag (not a port in registry URL)
		const hasTag = lastColon > 0 && !imageRef.substring(lastColon).includes('/');

		if (hasTag) {
			return {
				repo: imageRef.substring(0, lastColon),
				tag: imageRef.substring(lastColon + 1)
			};
		}
		return { repo: imageRef, tag: 'latest' };
	}

	async function performContainerAction(action: string, id: string) {
		isLoading[action as keyof typeof isLoading] = true;

		try {
			if (action === 'start') {
				handleApiResultWithCallbacks({
					result: await tryCatch(containerService.startContainer(id)),
					message: m.containers_start_failed(),
					setLoadingState: (value) => (isLoading.start = value),
					async onSuccess() {
						toast.success(m.containers_start_success());
						containers = await containerService.getContainers(requestOptions);
					}
				});
			} else if (action === 'stop') {
				handleApiResultWithCallbacks({
					result: await tryCatch(containerService.stopContainer(id)),
					message: m.containers_stop_failed(),
					setLoadingState: (value) => (isLoading.stop = value),
					async onSuccess() {
						toast.success(m.containers_stop_success());
						containers = await containerService.getContainers(requestOptions);
					}
				});
			} else if (action === 'restart') {
				handleApiResultWithCallbacks({
					result: await tryCatch(containerService.restartContainer(id)),
					message: m.containers_restart_failed(),
					setLoadingState: (value) => (isLoading.restart = value),
					async onSuccess() {
						toast.success(m.containers_restart_success());
						containers = await containerService.getContainers(requestOptions);
					}
				});
			}
		} catch (error) {
			console.error('Container action failed:', error);
			toast.error(m.containers_action_error());
			isLoading[action as keyof typeof isLoading] = false;
		}
	}

	async function handleRemoveContainer(id: string) {
		openConfirmDialog({
			title: m.containers_remove_confirm_title(),
			message: m.containers_remove_confirm_message(),
			checkboxes: [
				{
					id: 'force',
					label: m.containers_remove_force_label(),
					initialState: false
				},
				{
					id: 'volumes',
					label: m.containers_remove_volumes_label(),
					initialState: false
				}
			],
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async (checkboxStates) => {
					const force = !!checkboxStates.force;
					const volumes = !!checkboxStates.volumes;
					handleApiResultWithCallbacks({
						result: await tryCatch(containerService.deleteContainer(id, { force, volumes })),
						message: m.containers_remove_failed(),
						setLoadingState: (value) => (isLoading.remove = value),
						async onSuccess() {
							toast.success(m.containers_remove_success());
							containers = await containerService.getContainers(requestOptions);
						}
					});
				}
			}
		});
	}

	async function handleUpdateContainer(container: ContainerSummaryDto) {
		const containerName = container.names?.[0]?.replace(/^\//, '') || container.id.substring(0, 12);

		openConfirmDialog({
			title: m.containers_update_confirm_title(),
			message: m.containers_update_confirm_message({ name: containerName }),
			confirm: {
				label: m.containers_update_container(),
				action: async () => {
					isLoading.updating = true;
					try {
						toast.info(m.containers_update_pulling_image());

						// Use the new single container update endpoint
						const result = await containerService.updateContainer(container.id);

						if (result.failed > 0) {
							const failedItem = result.items?.find((item: any) => item.status === 'failed');
							toast.error(
								m.containers_update_failed({ name: containerName }) + (failedItem?.error ? `: ${failedItem.error}` : '')
							);
						} else if (result.updated > 0) {
							toast.success(m.containers_update_success({ name: containerName }));
						} else {
							toast.info(m.image_update_up_to_date_title());
						}

						// Refresh containers
						containers = await containerService.getContainers(requestOptions);
					} catch (error) {
						console.error('Container update failed:', error);
						toast.error(m.containers_update_failed({ name: containerName }));
					} finally {
						isLoading.updating = false;
					}
				}
			}
		});
	}

	const isAnyLoading = $derived(Object.values(isLoading).some((loading) => loading));

	const columns = [
		{ accessorKey: 'names', id: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'id', title: m.common_id(), cell: IdCell },
		{ accessorKey: 'state', title: m.common_state(), sortable: true, cell: StateCell },
		{ accessorKey: 'image', title: m.common_image(), sortable: true, cell: ImageCell },
		{ accessorKey: 'imageId', id: 'update', title: m.containers_update_column(), cell: UpdateCell },
		{ accessorKey: 'status', title: m.common_status() },
		{ accessorKey: 'ports', title: m.common_ports(), cell: PortsCell },
		{ accessorKey: 'created', title: m.common_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<ContainerSummaryDto>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: true },
		{ id: 'state', label: m.common_state(), defaultVisible: true },
		{ id: 'image', label: m.common_image(), defaultVisible: true },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'ports', label: m.common_ports(), defaultVisible: true },
		{ id: 'created', label: m.common_created(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let customSettings = $state<Record<string, unknown>>({});
	let groupByProject = $derived.by(() => {
		return (customSettings.groupByProject as boolean) ?? false;
	});

	function setGroupByProject(value: boolean) {
		customSettings = { ...customSettings, groupByProject: value };
	}

	function getProjectName(container: ContainerSummaryDto): string {
		const projectLabel = container.labels?.['com.docker.compose.project'];
		return projectLabel || 'No Project';
	}

	const groupedContainers = $derived(() => {
		if (!groupByProject) return null;

		const groups = new Map<string, ContainerSummaryDto[]>();

		for (const container of containers.data ?? []) {
			const projectName = getProjectName(container);
			if (!groups.has(projectName)) {
				groups.set(projectName, []);
			}
			groups.get(projectName)!.push(container);
		}

		const sortedGroups = Array.from(groups.entries()).sort(([a], [b]) => {
			if (a === 'No Project') return 1;
			if (b === 'No Project') return -1;
			return a.localeCompare(b);
		});

		return sortedGroups.length > 0 ? sortedGroups : null;
	});
</script>

{#snippet PortsCell({ item }: { item: ContainerSummaryDto })}
	<PortBadge ports={item.ports ?? []} {baseServerUrl} />
{/snippet}

{#snippet NameCell({ item }: { item: ContainerSummaryDto })}
	<a class="font-medium hover:underline" href="/containers/{item.id}">
		{#if item.names && item.names.length > 0}
			{item.names[0].startsWith('/') ? item.names[0].substring(1) : item.names[0]}
		{:else}
			{item.id.substring(0, 12)}
		{/if}
	</a>
{/snippet}

{#snippet IdCell({ item }: { item: ContainerSummaryDto })}
	<span class="font-mono text-sm">{String(item.id).substring(0, 12)}</span>
{/snippet}

{#snippet StateCell({ item }: { item: ContainerSummaryDto })}
	<StatusBadge
		variant={item.state === 'running' ? 'green' : item.state === 'exited' ? 'red' : 'amber'}
		text={capitalizeFirstLetter(item.state)}
	/>
{/snippet}

{#snippet ImageCell({ item }: { item: ContainerSummaryDto })}
	<Tooltip.Provider>
		<Tooltip.Root>
			<Tooltip.Trigger class="block max-w-[200px] cursor-default truncate text-left lg:max-w-[300px]">
				{item.image}
			</Tooltip.Trigger>
			<Tooltip.Content>
				<p>{item.image}</p>
			</Tooltip.Content>
		</Tooltip.Root>
	</Tooltip.Provider>
{/snippet}

{#snippet CreatedCell({ item }: { item: ContainerSummaryDto })}
	<span class="text-sm">
		{item.created ? format(new Date(item.created * 1000), 'PP p') : m.common_na()}
	</span>
{/snippet}

{#snippet UpdateCell({ item }: { item: ContainerSummaryDto })}
	{@const imageRef = parseImageRef(item.image)}
	<ImageUpdateItem
		updateInfo={item.updateInfo}
		imageId={item.imageId}
		repo={imageRef.repo}
		tag={imageRef.tag}
		onUpdateContainer={() => handleUpdateContainer(item)}
		debugHasUpdate={false}
	/>
{/snippet}

{#snippet ContainerMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: ContainerSummaryDto;
	mobileFieldVisibility: FieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item) => {
			const state = item.state;
			return {
				component: BoxIcon,
				variant: state === 'running' ? 'emerald' : state === 'exited' ? 'red' : 'amber'
			};
		}}
		title={(item) => {
			if (item.names && item.names.length > 0) {
				return item.names[0].startsWith('/') ? item.names[0].substring(1) : item.names[0];
			}
			return item.id.substring(0, 12);
		}}
		subtitle={(item) => ((mobileFieldVisibility.id ?? true) ? (item.id.length > 12 ? item.id : null) : null)}
		badges={[
			(item) =>
				(mobileFieldVisibility.state ?? true)
					? {
							variant: item.state === 'running' ? 'green' : item.state === 'exited' ? 'red' : 'amber',
							text: capitalizeFirstLetter(item.state)
						}
					: null
		]}
		fields={[
			{
				label: m.common_image(),
				getValue: (item: ContainerSummaryDto) => item.image,
				icon: ImageIcon,
				iconVariant: 'blue' as const,
				show: mobileFieldVisibility.image ?? true
			},
			{
				label: m.common_status(),
				getValue: (item: ContainerSummaryDto) => item.status,
				icon: ClockIcon,
				iconVariant: 'purple' as const,
				show: (mobileFieldVisibility.status ?? true) && item.status !== undefined
			}
		]}
		footer={(mobileFieldVisibility.created ?? true)
			? {
					label: m.common_created(),
					getValue: (item) => format(new Date(item.created * 1000), 'PP p'),
					icon: ClockIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={(item: ContainerSummaryDto) => goto(`/containers/${item.id}`)}
	>
		{#snippet children()}
			{#if (mobileFieldVisibility.ports ?? true) && item.ports && item.ports.length > 0}
				<div class="flex items-start gap-2.5 border-t pt-3">
					<div class="flex size-7 shrink-0 items-center justify-center rounded-lg bg-sky-500/10">
						<NetworkIcon class="size-3.5 text-sky-500" />
					</div>
					<div class="min-w-0 flex-1">
						<div class="text-muted-foreground text-[10px] font-medium tracking-wide uppercase">
							{m.common_ports()}
						</div>
						<div class="mt-1">
							<PortBadge ports={item.ports} {baseServerUrl} />
						</div>
					</div>
				</div>
			{/if}
		{/snippet}
	</UniversalMobileCard>
{/snippet}

{#snippet RowActions({ item }: { item: ContainerSummaryDto })}
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
				<DropdownMenu.Item onclick={() => goto(`/containers/${item.id}`)} disabled={isAnyLoading}>
					<ScanSearchIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>

				{#if item.updateInfo?.hasUpdate}
					<DropdownMenu.Item onclick={() => handleUpdateContainer(item)} disabled={isLoading.updating || isAnyLoading}>
						{#if isLoading.updating}
							<Spinner class="size-4" />
						{:else}
							<ArrowUpCircleIcon class="size-4" />
						{/if}
						{m.containers_update_container()}
					</DropdownMenu.Item>
				{/if}

				{#if item.state !== 'running'}
					<DropdownMenu.Item onclick={() => performContainerAction('start', item.id)} disabled={isLoading.start || isAnyLoading}>
						{#if isLoading.start}
							<Spinner class="size-4" />
						{:else}
							<PlayIcon class="size-4" />
						{/if}
						{m.common_start()}
					</DropdownMenu.Item>
				{:else}
					<DropdownMenu.Item
						onclick={() => performContainerAction('restart', item.id)}
						disabled={isLoading.restart || isAnyLoading}
					>
						{#if isLoading.restart}
							<Spinner class="size-4" />
						{:else}
							<RotateCcwIcon class="size-4" />
						{/if}
						{m.common_restart()}
					</DropdownMenu.Item>

					<DropdownMenu.Item onclick={() => performContainerAction('stop', item.id)} disabled={isLoading.stop || isAnyLoading}>
						{#if isLoading.stop}
							<Spinner class="size-4" />
						{:else}
							<StopCircleIcon class="size-4" />
						{/if}
						{m.common_stop()}
					</DropdownMenu.Item>
				{/if}

				<DropdownMenu.Separator />

				<DropdownMenu.Item
					variant="destructive"
					onclick={() => handleRemoveContainer(item.id)}
					disabled={isLoading.remove || isAnyLoading}
				>
					{#if isLoading.remove}
						<Spinner class="size-4" />
					{:else}
						<Trash2Icon class="size-4" />
					{/if}
					{m.common_remove()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-container-table"
	items={containers}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	bind:customSettings
	onRefresh={async (options) => (containers = await containerService.getContainers(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={ContainerMobileCardSnippet}
	selectionDisabled
	customViewOptions={CustomViewOptions}
	customTableView={groupByProject && groupedContainers() ? GroupedTableView : undefined}
/>

{#snippet CustomViewOptions()}
	<DropdownMenu.CheckboxItem bind:checked={() => groupByProject, (v) => setGroupByProject(!!v)}>
		{m.containers_group_by_project()}
	</DropdownMenu.CheckboxItem>
{/snippet}

{#snippet GroupedTableView({ table, renderPagination }: { table: TableType<ContainerSummaryDto>; renderPagination: import('svelte').Snippet })}
	<div class="flex items-center justify-end border-b px-6 py-2">
		<DataTableViewOptions {table} customViewOptions={CustomViewOptions} />
	</div>
	<div class=" space-y-4 px-6 py-2">
		{#each groupedContainers() ?? [] as [projectName, projectContainers] (projectName)}
			{@const projectContainerIds = new Set(projectContainers.map((c) => c.id))}
			{@const projectRows = table
				.getRowModel()
				.rows.filter((row) => projectContainerIds.has((row.original as ContainerSummaryDto).id))}

			<DropdownCard
				id={`container-project-${projectName}`}
				title={projectName}
				description={`${projectContainers.length} ${projectContainers.length === 1 ? 'container' : 'containers'}`}
				icon={FolderIcon}
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
							{#each projectRows as row (row.id)}
								<Table.Row data-state={(selectedIds ?? []).includes((row.original as ContainerSummaryDto).id) && 'selected'}>
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
					{#each projectRows as row (row.id)}
						{@render ContainerMobileCardSnippet({ item: row.original as ContainerSummaryDto, mobileFieldVisibility })}
					{:else}
						<div class="h-24 flex items-center justify-center text-center text-muted-foreground">
							{m.common_no_results_found()}
						</div>
					{/each}
				</div>
			</DropdownCard>
		{/each}
	</div>

	<div class="shrink-0 border-t px-2 py-4">
		{@render renderPagination()}
	</div>
{/snippet}
