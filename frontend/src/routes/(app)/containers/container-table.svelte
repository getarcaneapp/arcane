<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { goto } from '$app/navigation';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { format } from 'date-fns';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import type { ContainerSummaryDto } from '$lib/types/container.type';
	import type { ColumnSpec, BulkAction } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { PortBadge } from '$lib/components/badges/index.js';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { containerService } from '$lib/services/container-service';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import ImageUpdateItem from '$lib/components/image-update-item.svelte';
	import { PersistedState } from 'runed';
	import { onMount } from 'svelte';
	import { ContainerStatsManager } from './components/container-stats-manager.svelte';
	import ContainerStatsCell from './components/container-stats-cell.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import IconImage from '$lib/components/icon-image.svelte';
	import { getArcaneIconUrlFromLabels } from '$lib/utils/arcane-labels';
	import { createContainerActions } from './container-table.actions';
	import {
		getActionStatusMessage,
		getContainerDisplayName,
		getContainerIpAddress,
		getStateBadgeVariant,
		groupContainerByProject,
		parseImageRef,
		type ActionStatus
	} from './container-table.helpers';
	import {
		StartIcon,
		StopIcon,
		RefreshIcon,
		TrashIcon,
		EllipsisIcon,
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
	let actionStatus = $state<Record<string, ActionStatus>>({});

	let isBulkLoading = $state({
		start: false,
		stop: false,
		restart: false,
		remove: false
	});

	const statsManager = new ContainerStatsManager();

	async function refreshContainers(options: SearchPaginationSortRequest) {
		const result = await containerService.getContainers(options);
		containers = result;
		return result;
	}

	function getCurrentLimit() {
		return requestOptions?.pagination?.limit ?? containers?.pagination?.itemsPerPage ?? 20;
	}

	function setShowInternal(value: boolean) {
		const currentSetting = (customSettings.showInternalContainers as boolean) ?? false;
		const currentRequest = requestOptions?.includeInternal ?? false;
		if (value === currentSetting && value === currentRequest) return;

		customSettings = { ...customSettings, showInternalContainers: value };
		const nextOptions: SearchPaginationSortRequest = {
			...requestOptions,
			includeInternal: value,
			pagination: { page: 1, limit: getCurrentLimit() }
		};
		requestOptions = nextOptions;
		refreshContainers(nextOptions);
	}

	const {
		performContainerAction,
		handleRemoveContainer,
		handleUpdateContainer,
		handleBulkStart,
		handleBulkStop,
		handleBulkRestart,
		handleBulkRemove
	} = createContainerActions({
		getRequestOptions: () => requestOptions,
		setContainers: (next) => {
			containers = next;
		},
		setSelectedIds: (next) => {
			selectedIds = next;
		},
		actionStatus,
		isBulkLoading
	});

	const isAnyLoading = $derived(
		Object.values(actionStatus).some((status) => status !== '') || Object.values(isBulkLoading).some((loading) => loading)
	);

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let customSettings = $state<Record<string, unknown>>({});
	let showInternal = $derived.by(() => {
		return (customSettings.showInternalContainers as boolean) ?? false;
	});
	let collapsedGroupsState = $state<PersistedState<Record<string, boolean>> | null>(null);
	let collapsedGroups = $derived(collapsedGroupsState?.current ?? {});
	let columnVisibility = $state<Record<string, boolean>>({});

	const shouldConnect = $derived.by(() => {
		const cpuVisible = columnVisibility.cpuUsage !== false;
		const memoryVisible = columnVisibility.memoryUsage !== false;
		const statsVisible = cpuVisible || memoryVisible;

		if (!statsVisible) {
			return new Set<string>();
		}

		const runningContainers = containers.data?.filter((c) => c.state === 'running') ?? [];
		return new Set(runningContainers.map((c) => c.id));
	});

	const currentEnvId = $derived(environmentStore.selected?.id || '0');

	$effect(() => {
		statsManager.envId = currentEnvId;
		statsManager.targetIds = shouldConnect;
	});

	onMount(() => {
		collapsedGroupsState = new PersistedState<Record<string, boolean>>('container-groups-collapsed', {});

		const persistedInternal = (customSettings.showInternalContainers as boolean) ?? false;
		const currentInternal = requestOptions?.includeInternal ?? false;
		if (persistedInternal !== currentInternal) {
			setShowInternal(persistedInternal);
		}

		return () => {
			statsManager.destroy();
		};
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

	const columns = $derived([
		{ accessorKey: 'id', title: m.common_id(), cell: IdCell, hidden: true },
		{ accessorKey: 'names', id: 'name', title: m.common_name(), sortable: !groupByProject, cell: NameCell },
		{ accessorKey: 'image', title: m.common_image(), sortable: !groupByProject, cell: ImageCell },
		{ accessorKey: 'state', title: m.common_state(), sortable: !groupByProject, cell: StateCell },
		{
			id: 'updates',
			accessorFn: (row) => {
				if (row.updateInfo?.hasUpdate) return 'has_update';
				if (row.updateInfo?.error) return 'error';
				if (row.updateInfo) return 'up_to_date';
				return 'unknown';
			},
			title: m.containers_update_column(),
			sortable: false,
			cell: UpdatesCell
		},
		{
			accessorFn: (row) => statsManager.getCPUPercent(row.id) ?? -1,
			id: 'cpuUsage',
			title: m.containers_cpu_usage(),
			sortable: false,
			cell: CPUCell
		},
		{
			accessorFn: (row) => statsManager.getMemoryPercent(row.id) ?? -1,
			id: 'memoryUsage',
			title: m.containers_memory_usage(),
			sortable: false,
			cell: MemoryCell
		},
		{ accessorKey: 'status', title: m.common_status() },
		{ accessorKey: 'networkSettings', id: 'ipAddress', title: m.containers_ip_address(), sortable: false, cell: IPAddressCell },
		{ accessorKey: 'ports', title: m.common_ports(), cell: PortsCell },
		{ accessorKey: 'created', title: m.common_created(), sortable: !groupByProject, cell: CreatedCell }
	] satisfies ColumnSpec<ContainerSummaryDto>[]);

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'state', label: m.common_state(), defaultVisible: true },
		{ id: 'updates', label: m.containers_update_column(), defaultVisible: true },
		{ id: 'cpuUsage', label: m.containers_cpu_usage(), defaultVisible: false },
		{ id: 'memoryUsage', label: m.containers_memory_usage(), defaultVisible: false },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'image', label: m.common_image(), defaultVisible: true },
		{ id: 'ipAddress', label: m.containers_ip_address(), defaultVisible: false },
		{ id: 'ports', label: m.common_ports(), defaultVisible: true },
		{ id: 'created', label: m.common_created(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'start',
			label: m.containers_bulk_start({ count: selectedIds?.length ?? 0 }),
			action: 'start',
			onClick: handleBulkStart,
			loading: isBulkLoading.start,
			disabled: isAnyLoading,
			icon: StartIcon
		},
		{
			id: 'stop',
			label: m.containers_bulk_stop({ count: selectedIds?.length ?? 0 }),
			action: 'stop',
			onClick: handleBulkStop,
			loading: isBulkLoading.stop,
			disabled: isAnyLoading,
			icon: StopIcon
		},
		{
			id: 'restart',
			label: m.containers_bulk_restart({ count: selectedIds?.length ?? 0 }),
			action: 'restart',
			onClick: handleBulkRestart,
			loading: isBulkLoading.restart,
			disabled: isAnyLoading,
			icon: RefreshIcon
		},
		{
			id: 'remove',
			label: m.containers_bulk_remove({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleBulkRemove,
			loading: isBulkLoading.remove,
			disabled: isAnyLoading,
			icon: TrashIcon
		}
	]);

	// Icon for each group
	function getGroupIcon(_groupName: string) {
		return ProjectsIcon;
	}
</script>

{#snippet IPAddressCell({ item }: { item: ContainerSummaryDto })}
	{@const ip = getContainerIpAddress(item)}
	<span class="font-mono text-sm">{ip ?? m.common_na()}</span>
{/snippet}

{#snippet CPUCell({ item }: { item: ContainerSummaryDto })}
	<ContainerStatsCell
		value={statsManager.getCPUPercent(item.id)}
		loading={statsManager.isLoading(item.id) ?? false}
		stopped={item.state !== 'running'}
		type="cpu"
	/>
{/snippet}

{#snippet MemoryCell({ item }: { item: ContainerSummaryDto })}
	{@const memoryData = statsManager.getMemoryUsage(item.id)}
	<ContainerStatsCell value={memoryData?.usage} limit={memoryData?.limit} stopped={item.state !== 'running'} type="memory" />
{/snippet}

{#snippet PortsCell({ item }: { item: ContainerSummaryDto })}
	<PortBadge ports={item.ports ?? []} />
{/snippet}

{#snippet NameCell({ item }: { item: ContainerSummaryDto })}
	{@const displayName = getContainerDisplayName(item)}
	{@const iconUrl = getArcaneIconUrlFromLabels(item.labels)}
	<div class="flex items-center gap-2">
		<IconImage src={iconUrl} alt={displayName} fallback={BoxIcon} class="size-4" containerClass="size-7" />
		<a class="font-medium hover:underline" href="/containers/{item.id}">{displayName}</a>
	</div>
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

{#snippet UpdatesCell({ item }: { item: ContainerSummaryDto })}
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

{#snippet ImageCell({ item }: { item: ContainerSummaryDto })}
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger>
			<span class="block w-full cursor-default truncate text-left">
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
			const iconUrl = getArcaneIconUrlFromLabels(item.labels);
			const state = item.state;
			return {
				component: BoxIcon,
				variant: state === 'running' ? 'emerald' : state === 'exited' ? 'red' : 'amber',
				imageUrl: iconUrl ?? undefined,
				alt: getContainerDisplayName(item)
			};
		}}
		title={(item) => getContainerDisplayName(item)}
		subtitle={(item) => ((mobileFieldVisibility.id ?? true) ? (item.id.length > 12 ? item.id : null) : null)}
		badges={[
			(item) =>
				(mobileFieldVisibility.state ?? true)
					? {
							variant: getStateBadgeVariant(item.state),
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
			},
			{
				label: m.containers_cpu_usage(),
				getValue: (item: ContainerSummaryDto) => {
					const cpu = statsManager.getCPUPercent(item.id);
					if (item.state !== 'running') return m.common_na();
					if (cpu === undefined) return '...';
					return `${cpu.toFixed(1)}%`;
				},
				icon: ClockIcon,
				iconVariant: 'orange' as const,
				show: mobileFieldVisibility.cpuUsage ?? false
			},
			{
				label: m.containers_memory_usage(),
				getValue: (item: ContainerSummaryDto) => {
					const memData = statsManager.getMemoryUsage(item.id);
					if (item.state !== 'running') return m.common_na();
					if (!memData?.usage) return '...';
					return `${(memData.usage / 1024 / 1024).toFixed(0)} MB`;
				},
				icon: ClockIcon,
				iconVariant: 'purple' as const,
				show: mobileFieldVisibility.memoryUsage ?? false
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
		{#if ((mobileFieldVisibility.ports ?? true) && item.ports && item.ports.length > 0) || (mobileFieldVisibility.updates ?? true)}
			<div class="flex flex-row gap-4 border-t pt-3">
				{#if (mobileFieldVisibility.ports ?? true) && item.ports && item.ports.length > 0}
					<div class="flex min-w-0 flex-1 items-start gap-2.5">
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
				{#if mobileFieldVisibility.updates ?? true}
					{@const imageRef = parseImageRef(item.image)}
					<div class="flex min-w-0 flex-1 items-start gap-2.5">
						<div class="flex min-w-0 flex-col">
							<div class="text-muted-foreground text-[10px] font-medium tracking-wide uppercase">
								{m.images_updates()}
							</div>
							<div class="mt-1">
								<ImageUpdateItem
									updateInfo={item.updateInfo}
									imageId={item.id}
									repo={imageRef.repo}
									tag={imageRef.tag}
									onUpdateContainer={() => handleUpdateContainer(item)}
									debugHasUpdate={false}
								/>
							</div>
						</div>
					</div>
				{/if}
			</div>
		{/if}
	</UniversalMobileCard>
{/snippet}

{#snippet RowActions({ item }: { item: ContainerSummaryDto })}
	{@const status = actionStatus[item.id]}
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
				<DropdownMenu.Item onclick={() => goto(`/containers/${item.id}`)} disabled={isAnyLoading}>
					<InspectIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>

				<DropdownMenu.Separator />

				{#if item.updateInfo?.hasUpdate}
					<DropdownMenu.Item onclick={() => handleUpdateContainer(item)} disabled={status === 'updating' || isAnyLoading}>
						{#if status === 'updating'}
							<Spinner class="size-4" />
						{:else}
							<UpdateIcon class="size-4" />
							{m.common_update()}
						{/if}
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
				{/if}

				<DropdownMenu.Separator />

				<DropdownMenu.Item
					variant="destructive"
					onclick={() => handleRemoveContainer(item.id, getContainerDisplayName(item))}
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
	bind:columnVisibility
	onRefresh={refreshContainers}
	{columns}
	{mobileFields}
	{bulkActions}
	rowActions={RowActions}
	mobileCard={ContainerMobileCardSnippet}
	customViewOptions={CustomViewOptions}
	groupBy={groupByProject ? groupContainerByProject : undefined}
	groupIcon={groupByProject ? getGroupIcon : undefined}
	groupCollapsedState={collapsedGroups}
	onGroupToggle={toggleGroup}
/>

{#snippet CustomViewOptions()}
	<DropdownMenu.CheckboxItem bind:checked={() => groupByProject, (v) => setGroupByProject(!!v)}>
		{m.containers_group_by_project()}
	</DropdownMenu.CheckboxItem>
	<DropdownMenu.CheckboxItem bind:checked={() => showInternal, (v) => setShowInternal(!!v)}>
		{`${m.common_show()} ${m.internal()} ${m.containers_title()}`}
	</DropdownMenu.CheckboxItem>
{/snippet}
