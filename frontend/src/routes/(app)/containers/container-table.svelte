<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
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
	import { containerService } from '$lib/services/container-service';
	import DropdownCard from '$lib/components/dropdown-card.svelte';
	import type { Table as TableType } from '@tanstack/table-core';
	import * as Table from '$lib/components/ui/table/index.js';
	import FlexRender from '$lib/components/ui/data-table/flex-render.svelte';
	import DataTableToolbar from '$lib/components/arcane-table/arcane-table-toolbar.svelte';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import ImageUpdateItem from '$lib/components/image-update-item.svelte';
	import { PersistedState } from 'runed';
	import { onMount } from 'svelte';
	import {
		StartIcon,
		StopIcon,
		RefreshIcon,
		TrashIcon,
		EllipsisIcon,
		ArrowDownIcon,
		ArrowRightIcon,
		BoxIcon,
		ClockIcon,
		ImagesIcon,
		NetworksIcon,
		ProjectsIcon,
		InspectIcon,
		UpdateIcon
	} from '$lib/icons';

	type FieldVisibility = Record<string, boolean>;

	let {
		containers = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable()
	}: {
		containers: Paginated<ContainerSummaryDto>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	// Track action status per container ID (e.g., "starting", "stopping", "updating", "")
	type ActionStatus = 'starting' | 'stopping' | 'restarting' | 'updating' | 'removing' | '';
	let actionStatus = $state<Record<string, ActionStatus>>({});

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

	function getActionStatusMessage(status: ActionStatus): string {
		const messages: Record<ActionStatus, () => string> = {
			starting: () => m.common_action_starting(),
			stopping: () => m.common_action_stopping(),
			restarting: () => m.common_action_restarting(),
			updating: () => m.common_action_updating(),
			removing: () => m.common_action_removing(),
			'': () => ''
		};
		return messages[status]();
	}

	function getStateBadgeVariant(state: string): 'green' | 'red' | 'amber' {
		if (state === 'running') return 'green';
		if (state === 'exited') return 'red';
		return 'amber';
	}

	async function performContainerAction(action: 'start' | 'stop' | 'restart', id: string) {
		// Set action status for this specific container
		if (action === 'start') {
			actionStatus[id] = 'starting';
		} else if (action === 'stop') {
			actionStatus[id] = 'stopping';
		} else if (action === 'restart') {
			actionStatus[id] = 'restarting';
		}

		try {
			if (action === 'start') {
				handleApiResultWithCallbacks({
					result: await tryCatch(containerService.startContainer(id)),
					message: m.containers_start_failed(),
					setLoadingState: (value) => {
						actionStatus[id] = value ? 'starting' : '';
					},
					async onSuccess() {
						toast.success(m.containers_start_success());
						containers = await containerService.getContainers(requestOptions);
					}
				});
			} else if (action === 'stop') {
				handleApiResultWithCallbacks({
					result: await tryCatch(containerService.stopContainer(id)),
					message: m.containers_stop_failed(),
					setLoadingState: (value) => {
						actionStatus[id] = value ? 'stopping' : '';
					},
					async onSuccess() {
						toast.success(m.containers_stop_success());
						containers = await containerService.getContainers(requestOptions);
					}
				});
			} else if (action === 'restart') {
				handleApiResultWithCallbacks({
					result: await tryCatch(containerService.restartContainer(id)),
					message: m.containers_restart_failed(),
					setLoadingState: (value) => {
						actionStatus[id] = value ? 'restarting' : '';
					},
					async onSuccess() {
						toast.success(m.containers_restart_success());
						containers = await containerService.getContainers(requestOptions);
					}
				});
			}
		} catch (error) {
			console.error('Container action failed:', error);
			toast.error(m.containers_action_error());
			actionStatus[id] = '';
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
					actionStatus[id] = 'removing';
					handleApiResultWithCallbacks({
						result: await tryCatch(containerService.deleteContainer(id, { force, volumes })),
						message: m.containers_remove_failed(),
						setLoadingState: (value) => {
							actionStatus[id] = value ? 'removing' : '';
						},
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
					actionStatus[container.id] = 'updating';
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
						actionStatus[container.id] = '';
					}
				}
			}
		});
	}

	const isAnyLoading = $derived(Object.values(actionStatus).some((status) => status !== ''));

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let customSettings = $state<Record<string, unknown>>({});
	let collapsedGroupsState = $state<PersistedState<Record<string, boolean>> | null>(null);
	let collapsedGroups = $derived(collapsedGroupsState?.current ?? {});

	onMount(() => {
		collapsedGroupsState = new PersistedState<Record<string, boolean>>('container-groups-collapsed', {});
	});

	let groupByProject = $derived.by(() => {
		return (customSettings.groupByProject as boolean) ?? false;
	});

	function setGroupByProject(value: boolean) {
		customSettings = { ...customSettings, groupByProject: value };
	}

	function toggleGroup(groupName: string) {
		if (!collapsedGroupsState) return;
		collapsedGroupsState.current = {
			...collapsedGroupsState.current,
			[groupName]: !collapsedGroupsState.current[groupName]
		};
	}

	function getContainerIpAddress(container: ContainerSummaryDto): string | null {
		const networks = container.networkSettings?.networks;
		if (!networks) return null;
		for (const networkName in networks) {
			const network = networks[networkName];
			if (network?.ipAddress) return network.ipAddress;
		}
		return null;
	}

	const columns = $derived([
		{ accessorKey: 'id', title: m.common_id(), cell: IdCell, hidden: true },
		{ accessorKey: 'names', id: 'name', title: m.common_name(), sortable: !groupByProject, cell: NameCell },
		{ accessorKey: 'state', title: m.common_state(), sortable: !groupByProject, cell: StateCell },
		{ accessorKey: 'image', title: m.common_image(), sortable: !groupByProject, cell: ImageCell },
		{ accessorKey: 'status', title: m.common_status() },
		{ accessorKey: 'imageId', id: 'update', title: m.containers_update_column(), cell: UpdateCell },
		{ accessorKey: 'networkSettings', id: 'ipAddress', title: m.containers_ip_address(), sortable: false, cell: IPAddressCell },
		{ accessorKey: 'ports', title: m.common_ports(), cell: PortsCell },
		{ accessorKey: 'created', title: m.common_created(), sortable: !groupByProject, cell: CreatedCell }
	] satisfies ColumnSpec<ContainerSummaryDto>[]);

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'state', label: m.common_state(), defaultVisible: true },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'image', label: m.common_image(), defaultVisible: true },
		{ id: 'ipAddress', label: m.containers_ip_address(), defaultVisible: false },
		{ id: 'ports', label: m.common_ports(), defaultVisible: true },
		{ id: 'created', label: m.common_created(), defaultVisible: true }
	];

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

{#snippet IPAddressCell({ item }: { item: ContainerSummaryDto })}
	{@const ip = getContainerIpAddress(item)}
	<span class="font-mono text-sm">{ip ?? m.common_na()}</span>
{/snippet}

{#snippet PortsCell({ item }: { item: ContainerSummaryDto })}
	<PortBadge ports={item.ports ?? []} />
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
	<span class="font-mono text-sm">{String(item.id)}</span>
{/snippet}

{#snippet StateCell({ item }: { item: ContainerSummaryDto })}
	{@const status = actionStatus[item.id]}
	<div class="flex items-center gap-2">
		{#if status}
			<div class="flex items-center gap-1.5">
				<Spinner class="size-3.5" />
				<span class="text-muted-foreground text-xs font-medium">
					{getActionStatusMessage(status)}
				</span>
			</div>
		{:else}
			<StatusBadge variant={getStateBadgeVariant(item.state)} text={capitalizeFirstLetter(item.state)} />
		{/if}
		<div class="flex items-center gap-1">
			{#if !status && item.state !== 'running'}
				<ArcaneButton
					action="base"
					tone="outline"
					size="sm"
					class="size-7 border-transparent bg-transparent p-0 text-green-600 shadow-none hover:bg-green-600/10 hover:text-green-500"
					onclick={() => performContainerAction('start', item.id)}
					disabled={isAnyLoading}
					icon={StartIcon}
					title={m.common_start()}
				/>
			{:else if !status && item.state === 'running'}
				<ArcaneButton
					action="base"
					tone="outline"
					size="sm"
					class="size-7 border-transparent bg-transparent p-0 text-red-600 shadow-none hover:bg-red-600/10 hover:text-red-500"
					onclick={() => performContainerAction('stop', item.id)}
					disabled={isAnyLoading}
					title={m.common_stop()}
					icon={StopIcon}
				/>
			{/if}
			{#if !status && item.updateInfo?.hasUpdate}
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					class="size-7 p-0"
					onclick={() => handleUpdateContainer(item)}
					disabled={isAnyLoading}
					title={m.containers_update_container()}
					icon={UpdateIcon}
				/>
			{/if}
		</div>
	</div>
{/snippet}

{#snippet ImageCell({ item }: { item: ContainerSummaryDto })}
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger>
			<span class="block max-w-[200px] cursor-default truncate text-left lg:max-w-[300px]">
				{item.image}
			</span>
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content>
			<p>{item.image}</p>
		</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
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
				icon: ImagesIcon,
				iconVariant: 'blue' as const,
				show: mobileFieldVisibility.image ?? true
			},
			{
				label: m.common_status(),
				getValue: (item: ContainerSummaryDto) => item.status,
				icon: ClockIcon,
				iconVariant: 'purple' as const,
				show: (mobileFieldVisibility.status ?? true) && item.status !== undefined
			},
			{
				label: m.containers_ip_address(),
				getValue: (item: ContainerSummaryDto) => getContainerIpAddress(item) ?? m.common_na(),
				icon: NetworksIcon,
				iconVariant: 'sky' as const,
				type: 'mono' as const,
				show: mobileFieldVisibility.ipAddress ?? false
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
						<NetworksIcon class="size-3.5 text-sky-500" />
					</div>
					<div class="min-w-0 flex-1">
						<div class="text-muted-foreground text-[10px] font-medium tracking-wide uppercase">
							{m.common_ports()}
						</div>
						<div class="mt-1">
							<PortBadge ports={item.ports} />
						</div>
					</div>
				</div>
			{/if}
		{/snippet}
	</UniversalMobileCard>
{/snippet}

{#snippet RowActions({ item }: { item: ContainerSummaryDto })}
	{@const status = actionStatus[item.id]}
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
				<DropdownMenu.Item onclick={() => goto(`/containers/${item.id}`)} disabled={isAnyLoading}>
					<InspectIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>

				{#if item.updateInfo?.hasUpdate}
					<DropdownMenu.Item onclick={() => handleUpdateContainer(item)} disabled={status === 'updating' || isAnyLoading}>
						{#if status === 'updating'}
							<Spinner class="size-4" />
						{:else}
							<UpdateIcon class="size-4" />
						{/if}
						{m.containers_update_container()}
					</DropdownMenu.Item>
				{/if}

				{#if item.state !== 'running'}
					<DropdownMenu.Item
						onclick={() => performContainerAction('start', item.id)}
						disabled={status === 'starting' || isAnyLoading}
					>
						{#if status === 'starting'}
							<Spinner class="size-4" />
						{:else}
							<StartIcon class="size-4" />
						{/if}
						{m.common_start()}
					</DropdownMenu.Item>
				{:else}
					<DropdownMenu.Item
						onclick={() => performContainerAction('restart', item.id)}
						disabled={status === 'restarting' || isAnyLoading}
					>
						{#if status === 'restarting'}
							<Spinner class="size-4" />
						{:else}
							<RefreshIcon class="size-4" />
						{/if}
						{m.common_restart()}
					</DropdownMenu.Item>

					<DropdownMenu.Item
						onclick={() => performContainerAction('stop', item.id)}
						disabled={status === 'stopping' || isAnyLoading}
					>
						{#if status === 'stopping'}
							<Spinner class="size-4" />
						{:else}
							<StopIcon class="size-4" />
						{/if}
						{m.common_stop()}
					</DropdownMenu.Item>
				{/if}

				<DropdownMenu.Separator />

				<DropdownMenu.Item
					variant="destructive"
					onclick={() => handleRemoveContainer(item.id)}
					disabled={status === 'removing' || isAnyLoading}
				>
					{#if status === 'removing'}
						<Spinner class="size-4" />
					{:else}
						<TrashIcon class="size-4" />
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

{#snippet GroupedTableView({
	table,
	renderPagination,
	mobileFieldsForOptions,
	onToggleMobileField
}: {
	table: TableType<ContainerSummaryDto>;
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
						{#each groupedContainers() ?? [] as [projectName, projectContainers] (projectName)}
							<Table.Row
								class="bg-muted/50 hover:bg-muted/60 cursor-pointer transition-colors"
								onclick={() => toggleGroup(projectName)}
							>
								<Table.Cell colspan={table.getAllColumns().length} class="py-3 font-medium">
									<div class="flex items-center gap-2">
										{#if collapsedGroups[projectName]}
											<ArrowRightIcon class="text-muted-foreground size-4" />
										{:else}
											<ArrowDownIcon class="text-muted-foreground size-4" />
										{/if}
										<ProjectsIcon class="text-muted-foreground size-4" />
										<span>{projectName}</span>
										<span class="text-muted-foreground text-xs font-normal">({projectContainers.length})</span>
									</div>
								</Table.Cell>
							</Table.Row>

							{#if !collapsedGroups[projectName]}
								{@const projectContainerIds = new Set(projectContainers.map((c) => c.id))}
								{@const projectRows = table
									.getRowModel()
									.rows.filter((row) => projectContainerIds.has((row.original as ContainerSummaryDto).id))}

								{#each projectRows as row (row.id)}
									<Table.Row
										data-state={(selectedIds ?? []).includes((row.original as ContainerSummaryDto).id) && 'selected'}
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
			{#each groupedContainers() ?? [] as [projectName, projectContainers] (projectName)}
				{@const projectContainerIds = new Set(projectContainers.map((c) => c.id))}
				{@const projectRows = table
					.getRowModel()
					.rows.filter((row) => projectContainerIds.has((row.original as ContainerSummaryDto).id))}

				<DropdownCard
					id={`container-project-${projectName}`}
					title={projectName}
					description={`${projectContainers.length} ${projectContainers.length === 1 ? 'container' : 'containers'}`}
					icon={ProjectsIcon}
				>
					{#each projectRows as row (row.id)}
						{@render ContainerMobileCardSnippet({ item: row.original as ContainerSummaryDto, mobileFieldVisibility })}
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
