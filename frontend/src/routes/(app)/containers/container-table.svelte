<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { goto } from '$app/navigation';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '$lib/components/arcane-table/row-actions-menu.svelte';
	import ContainerActionMenuItem from '$lib/components/arcane-table/cells/container-action-menu-item.svelte';
	import type { SearchPaginationSortRequest } from '$lib/types/shared';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { format } from 'date-fns';
	import { truncateImageDigest } from '$lib/utils/formatting';
	import type { ContainerSummaryDto } from '$lib/types/docker';
	import type { ColumnSpec, BulkAction } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { PortBadge } from '$lib/components/badges/index.js';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import {
		containerService,
		type ContainerListRequestOptions,
		type ContainersPaginatedResponse
	} from '$lib/services/container-service';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import ImageUpdateItem from '$lib/components/image-update-item.svelte';
	import { PersistedState } from 'runed';
	import { onMount } from 'svelte';
	import { mode } from 'mode-watcher';
	import { ContainerStatsManager } from './components/container-stats-manager.svelte';
	import ContainerStatsSync from './components/container-stats-sync.svelte';
	import ContainerStatsCell from './components/container-stats-cell.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import IconImage from '$lib/components/icon-image.svelte';
	import { getContainerIpAddresses, getThemedIconUrl } from '$lib/utils/docker';
	import { hasAnyLoadingState } from '$lib/utils/bulk-actions';
	import { createContainerActions } from './container-table.actions';
	import {
		getActionStatusMessage,
		getContainerDisplayName,
		getProjectName,
		getStateBadgeVariant,
		parseImageRef,
		getContainerStatusLabel,
		type ActionStatus
	} from './container-table.helpers';
	import {
		StartIcon,
		StopIcon,
		RefreshIcon,
		TrashIcon,
		BoxIcon,
		ClockIcon,
		ImagesIcon,
		NetworksIcon,
		ProjectsIcon,
		InspectIcon,
		UpdateIcon,
		RedeployIcon,
		PauseIcon,
		PlayIcon,
		ZapIcon
	} from '$lib/icons';
	import KillContainerDialog from './components/kill-container-dialog.svelte';

	type FieldVisibility = Record<string, boolean>;

	let {
		environmentId,
		containers = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		groupByProject = $bindable(false),
		withoutFilters = false,
		onRefreshData
	}: {
		environmentId: string;
		containers: ContainersPaginatedResponse;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		groupByProject?: boolean;
		withoutFilters?: boolean;
		onRefreshData?: (options: ContainerListRequestOptions) => Promise<ContainersPaginatedResponse>;
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

	function buildGroupedRequest(options: SearchPaginationSortRequest, grouped = groupByProject): ContainerListRequestOptions {
		return {
			...options,
			groupByProject: grouped
		};
	}

	async function refreshContainers(options: SearchPaginationSortRequest, grouped = groupByProject) {
		const request = buildGroupedRequest(options, grouped);
		if (onRefreshData) {
			const result = await onRefreshData(request);
			containers = result;
			return result;
		}
		const result = await containerService.getContainers(request);
		containers = result;
		return result;
	}

	function getCurrentLimit() {
		return requestOptions?.pagination?.limit ?? containers?.pagination?.itemsPerPage ?? 20;
	}

	function setShowInternal(value: boolean) {
		const currentSetting = (customSettings['showInternalContainers'] as boolean) ?? false;
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
		handleRedeployContainer,
		handleBulkStart,
		handleBulkStop,
		handleBulkRestart,
		handleBulkRemove
	} = createContainerActions({
		setContainers: (next) => {
			containers = next;
		},
		setSelectedIds: (next) => {
			selectedIds = next;
		},
		refreshContainers: () => refreshContainers(requestOptions),
		actionStatus,
		isBulkLoading
	});

	const isAnyLoading = $derived(hasAnyLoadingState(actionStatus, isBulkLoading));

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let customSettings = $state<Record<string, unknown>>({});
	let showInternal = $derived.by(() => {
		return (customSettings['showInternalContainers'] as boolean) ?? false;
	});
	let hideExposedPorts = $derived.by(() => {
		return (customSettings['hideExposedPorts'] as boolean) ?? false;
	});
	let collapsedGroupsState = $state<PersistedState<Record<string, boolean>> | null>(null);
	let collapsedGroups = $derived(collapsedGroupsState?.current ?? {});
	let columnVisibility = $state<Record<string, boolean>>({});
	const backendGroupedRows = $derived(
		containers.groups?.map((group) => ({
			groupName: group.groupName,
			items: group.items
		})) ?? null
	);

	const shouldConnect = $derived.by(() => {
		if (!resourcesCurrent) {
			return new Set<string>();
		}
		const cpuVisible = columnVisibility['cpuUsage'] !== false;
		const memoryVisible = columnVisibility['memoryUsage'] !== false;
		const statsVisible = cpuVisible || memoryVisible;

		if (!statsVisible) {
			return new Set<string>();
		}

		const runningContainers = containers.data?.filter((c) => c.state === 'running') ?? [];
		return new Set(runningContainers.map((c) => c.id));
	});

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const resourcesCurrent = $derived(environmentId === currentEnvId);
	const canUpdateContainers = $derived(hasPermission('containers:autoupdate', currentEnvId));
	const canKillContainers = $derived(hasPermission('containers:kill', currentEnvId));
	const canPauseContainers = $derived(hasPermission('containers:pause', currentEnvId));

	// The dialog is mounted only while a row is targeted, so it gets fresh state on
	// every open; clearing the target unmounts it.
	let killTarget = $state<ContainerSummaryDto | null>(null);

	function openKillDialog(container: ContainerSummaryDto) {
		killTarget = container;
	}

	onMount(() => {
		collapsedGroupsState = new PersistedState<Record<string, boolean>>('container-groups-collapsed', {});

		const persistedInternal = (customSettings['showInternalContainers'] as boolean) ?? false;
		const currentInternal = requestOptions?.includeInternal ?? false;
		if (persistedInternal !== currentInternal) {
			setShowInternal(persistedInternal);
		}

		const persistedGroupByProject = (customSettings['groupByProject'] as boolean) ?? false;
		if (persistedGroupByProject !== groupByProject) {
			groupByProject = persistedGroupByProject;
		}

		if (persistedGroupByProject) {
			const nextOptions: SearchPaginationSortRequest = {
				...requestOptions,
				pagination: { page: 1, limit: getCurrentLimit() }
			};
			requestOptions = nextOptions;
			void refreshContainers(nextOptions, true);
		}

		return () => {
			statsManager.destroy();
		};
	});

	function setGroupByProject(value: boolean) {
		customSettings = { ...customSettings, groupByProject: value };
		groupByProject = value;
		const nextOptions: SearchPaginationSortRequest = {
			...requestOptions,
			pagination: { page: 1, limit: getCurrentLimit() }
		};
		requestOptions = nextOptions;
		void refreshContainers(nextOptions, value);
	}

	function toggleGroup(groupName: string) {
		if (!collapsedGroupsState) return;
		collapsedGroupsState.current = {
			...collapsedGroupsState.current,
			// Groups with no recorded state render collapsed, so toggle from that default
			[groupName]: !(collapsedGroupsState.current[groupName] ?? true)
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
		{ accessorKey: 'ports', title: m.common_ports(), sortable: !groupByProject, cell: PortsCell },
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
			disabled: !resourcesCurrent || isAnyLoading,
			icon: StartIcon
		},
		{
			id: 'stop',
			label: m.containers_bulk_stop({ count: selectedIds?.length ?? 0 }),
			action: 'stop',
			onClick: handleBulkStop,
			loading: isBulkLoading.stop,
			disabled: !resourcesCurrent || isAnyLoading,
			icon: StopIcon
		},
		{
			id: 'restart',
			label: m.containers_bulk_restart({ count: selectedIds?.length ?? 0 }),
			action: 'restart',
			onClick: handleBulkRestart,
			loading: isBulkLoading.restart,
			disabled: !resourcesCurrent || isAnyLoading,
			icon: RefreshIcon
		},
		{
			id: 'remove',
			label: m.containers_bulk_remove({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleBulkRemove,
			loading: isBulkLoading.remove,
			disabled: !resourcesCurrent || isAnyLoading,
			icon: TrashIcon
		}
	]);

	// Icon for each group
	function getGroupIcon(_groupName: string) {
		return ProjectsIcon;
	}

	function getGroupName(item: ContainerSummaryDto): string {
		return getProjectName(item);
	}
</script>

{#if resourcesCurrent}
	<ContainerStatsSync {statsManager} envId={environmentId} targetIds={shouldConnect} />
{/if}

{#snippet IPAddressCell({ item }: { item: ContainerSummaryDto })}
	{@const ipAddresses = getContainerIpAddresses(item)}
	{#if ipAddresses.length > 0}
		<div class="space-y-0.5">
			{#each ipAddresses as ipAddress (ipAddress)}
				<div class="font-mono text-sm leading-tight">{ipAddress}</div>
			{/each}
		</div>
	{:else}
		<span class="font-mono text-sm">{m.common_na()}</span>
	{/if}
{/snippet}

{#snippet IPAddressesField(ipAddresses: string[])}
	{#if ipAddresses.length > 0}
		<span class="flex flex-col gap-0.5">
			{#each ipAddresses as ipAddress (ipAddress)}
				<span class="font-mono text-xs leading-tight">{ipAddress}</span>
			{/each}
		</span>
	{:else}
		<span class="font-mono text-xs">{m.common_na()}</span>
	{/if}
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
	<PortBadge ports={item.ports ?? []} hideExposed={hideExposedPorts} wrap={false} />
{/snippet}

{#snippet NameCell({ item }: { item: ContainerSummaryDto })}
	{@const displayName = getContainerDisplayName(item)}
	{@const iconUrl = getThemedIconUrl(item, mode.current)}
	<div class="flex items-center gap-2">
		<IconImage src={iconUrl} alt={displayName} fallback={BoxIcon} class="size-6" containerClass="size-8" />
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
			<StatusBadge variant={getStateBadgeVariant(item.state)} text={getContainerStatusLabel(item.state)} />
		{/if}
		<div class="flex items-center gap-1">
			{#if !status && item.state !== 'running'}
				<ArcaneButton
					action="base"
					tone="outline"
					size="sm"
					class="size-7 border-transparent bg-transparent p-0 text-green-600 shadow-none hover:bg-green-600/10 hover:text-green-500"
					onclick={() => performContainerAction('start', item.id)}
					disabled={!resourcesCurrent || isAnyLoading}
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
					disabled={!resourcesCurrent || isAnyLoading}
					title={m.common_stop()}
					icon={StopIcon}
				/>
			{/if}
			{#if !status && item.updateInfo?.hasUpdate && canUpdateContainers}
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					class="size-7 p-0"
					onclick={() => handleUpdateContainer(item)}
					disabled={!resourcesCurrent || isAnyLoading}
					title={m.containers_update_container()}
					icon={UpdateIcon}
				/>
			{/if}
		</div>
	</div>
{/snippet}

{#snippet ContainerUpdateItem(item: ContainerSummaryDto)}
	{@const imageRef = parseImageRef(item.image)}
	<ImageUpdateItem
		updateInfo={item.updateInfo}
		imageId={item.imageId}
		repo={imageRef.repo}
		tag={imageRef.tag}
		onUpdateContainer={canUpdateContainers ? () => handleUpdateContainer(item) : undefined}
		debugHasUpdate={false}
	/>
{/snippet}

{#snippet UpdatesCell({ item }: { item: ContainerSummaryDto })}
	{@render ContainerUpdateItem(item)}
{/snippet}

{#snippet ImageCell({ item }: { item: ContainerSummaryDto })}
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger class="flex w-full min-w-0">
			<span class="min-w-0 flex-1 cursor-default truncate text-left font-mono text-xs">
				{truncateImageDigest(item.image)}
			</span>
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content>
			<p class="max-w-xl break-all">{item.image}</p>
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
			const iconUrl = getThemedIconUrl(item, mode.current);
			const state = item.state;
			return {
				component: BoxIcon,
				variant: state === 'running' ? 'emerald' : state === 'exited' ? 'red' : 'amber',
				imageUrl: iconUrl ?? undefined,
				alt: getContainerDisplayName(item)
			};
		}}
		title={(item) => getContainerDisplayName(item)}
		subtitle={(item) => ((mobileFieldVisibility['id'] ?? true) ? (item.id.length > 12 ? item.id : null) : null)}
		badges={[
			(item) =>
				(mobileFieldVisibility['state'] ?? true)
					? {
							variant: getStateBadgeVariant(item.state),
							text: getContainerStatusLabel(item.state)
						}
					: null
		]}
		fields={[
			{
				label: m.common_image(),
				getValue: (item: ContainerSummaryDto) => item.image,
				icon: ImagesIcon,
				iconVariant: 'blue' as const,
				show: mobileFieldVisibility['image'] ?? true
			},
			{
				label: m.common_status(),
				getValue: (item: ContainerSummaryDto) => item.status,
				icon: ClockIcon,
				iconVariant: 'purple' as const,
				show: (mobileFieldVisibility['status'] ?? true) && item.status !== undefined
			},
			{
				label: m.containers_ip_address(),
				getValue: (item: ContainerSummaryDto) => getContainerIpAddresses(item),
				icon: NetworksIcon,
				iconVariant: 'sky' as const,
				type: 'component' as const,
				component: IPAddressesField,
				show: mobileFieldVisibility['ipAddress'] ?? false
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
				show: mobileFieldVisibility['cpuUsage'] ?? false
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
				show: mobileFieldVisibility['memoryUsage'] ?? false
			}
		]}
		footer={(mobileFieldVisibility['created'] ?? true)
			? {
					label: m.common_created(),
					getValue: (item) => format(new Date(item.created * 1000), 'PP p'),
					icon: ClockIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={(item: ContainerSummaryDto) => goto(`/containers/${item.id}`)}
	>
		{#if ((mobileFieldVisibility['ports'] ?? true) && item.ports && item.ports.length > 0) || (mobileFieldVisibility['updates'] ?? true)}
			<div class="flex flex-row gap-4 border-t pt-3">
				{#if (mobileFieldVisibility['ports'] ?? true) && item.ports && item.ports.length > 0}
					<div class="flex min-w-0 flex-1 items-start gap-2.5">
						<div class="flex size-7 shrink-0 items-center justify-center rounded-lg bg-sky-500/10">
							<NetworksIcon class="size-3.5 text-sky-500" />
						</div>
						<div class="min-w-0 flex-1">
							<div class="text-muted-foreground text-[10px] font-medium tracking-wide uppercase">
								{m.common_ports()}
							</div>
							<div class="mt-1">
								<PortBadge ports={item.ports} hideExposed={hideExposedPorts} />
							</div>
						</div>
					</div>
				{/if}
				{#if mobileFieldVisibility['updates'] ?? true}
					<div class="flex min-w-0 flex-1 items-start gap-2.5">
						<div class="flex min-w-0 flex-col">
							<div class="text-muted-foreground text-[10px] font-medium tracking-wide uppercase">
								{m.images_updates()}
							</div>
							<div class="mt-1">
								{@render ContainerUpdateItem(item)}
							</div>
						</div>
					</div>
				{/if}
			</div>
		{/if}
	</UniversalMobileCard>
{/snippet}

{#snippet RowActions({ item }: { item: ContainerSummaryDto })}
	{#if resourcesCurrent}
		{@const status = actionStatus[item.id]}
		<RowActionsMenu>
			<DropdownMenu.Item onclick={() => goto(`/containers/${item.id}`)} disabled={isAnyLoading}>
				<InspectIcon class="size-4" />
				{m.common_inspect()}
			</DropdownMenu.Item>

			<DropdownMenu.Separator />

			{#if item.updateInfo?.hasUpdate && canUpdateContainers}
				<DropdownMenu.Item onclick={() => handleUpdateContainer(item)} disabled={status === 'updating' || isAnyLoading}>
					{#if status === 'updating'}
						<Spinner class="size-4" />
					{:else}
						<UpdateIcon class="size-4" />
						{m.common_update()}
					{/if}
				</DropdownMenu.Item>
			{/if}
			{#if item.state === 'paused'}
				{#if canPauseContainers}
					<DropdownMenu.Item
						onclick={() => performContainerAction('unpause', item.id)}
						disabled={status === 'unpausing' || isAnyLoading}
					>
						{#if status === 'unpausing'}
							<Spinner class="size-4" />
						{:else}
							<PlayIcon class="size-4" />
						{/if}
						{m.common_unpause()}
					</DropdownMenu.Item>
				{/if}
			{:else if item.state !== 'running'}
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
				<ContainerActionMenuItem
					icon={StopIcon}
					label={m.common_stop()}
					onclick={() => performContainerAction('stop', item.id)}
					loading={status === 'stopping'}
					disabled={status === 'stopping' || isAnyLoading}
				/>

				<ContainerActionMenuItem
					icon={RefreshIcon}
					label={m.common_restart()}
					onclick={() => performContainerAction('restart', item.id)}
					loading={status === 'restarting'}
					disabled={status === 'restarting' || isAnyLoading}
				/>

				{#if canPauseContainers}
					<DropdownMenu.Item
						onclick={() => performContainerAction('pause', item.id)}
						disabled={status === 'pausing' || isAnyLoading}
					>
						{#if status === 'pausing'}
							<Spinner class="size-4" />
						{:else}
							<PauseIcon class="size-4" />
						{/if}
						{m.common_pause()}
					</DropdownMenu.Item>
				{/if}
			{/if}

			{#if (item.state === 'running' || item.state === 'paused') && canKillContainers}
				<DropdownMenu.Item onclick={() => openKillDialog(item)} disabled={isAnyLoading}>
					<ZapIcon class="size-4" />
					{m.common_kill()}
				</DropdownMenu.Item>
			{/if}

			{#if item.redeployDisabled}
				<DropdownMenu.Item disabled title={m.common_redeploy_disabled_arcane_self()}>
					<RedeployIcon class="size-4 opacity-50" />
					{m.common_redeploy()}
				</DropdownMenu.Item>
			{:else}
				<DropdownMenu.Item onclick={() => handleRedeployContainer(item)} disabled={status === 'redeploying' || isAnyLoading}>
					{#if status === 'redeploying'}
						<Spinner class="size-4" />
					{:else}
						<RedeployIcon class="size-4" />
					{/if}
					{m.common_redeploy()}
				</DropdownMenu.Item>
			{/if}

			<DropdownMenu.Separator />

			<ContainerActionMenuItem
				icon={TrashIcon}
				label={m.common_remove()}
				onclick={() => handleRemoveContainer(item.id, getContainerDisplayName(item))}
				loading={status === 'removing'}
				disabled={status === 'removing' || isAnyLoading}
				destructive
			/>
		</RowActionsMenu>
	{/if}
{/snippet}

<ArcaneTable
	persistKey="arcane-container-table"
	items={containers}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	bind:customSettings
	bind:columnVisibility
	{withoutFilters}
	onRefresh={refreshContainers}
	{columns}
	{mobileFields}
	{bulkActions}
	rowActions={RowActions}
	mobileCard={ContainerMobileCardSnippet}
	customViewOptions={CustomViewOptions}
	groupedRows={groupByProject ? backendGroupedRows : null}
	groupBy={groupByProject && !backendGroupedRows ? getGroupName : undefined}
	groupIcon={groupByProject ? getGroupIcon : undefined}
	groupCollapsedState={collapsedGroups}
	onGroupToggle={toggleGroup}
/>

{#snippet CustomViewOptions()}
	<DropdownMenu.CheckboxItem checked={groupByProject} onCheckedChange={(v) => setGroupByProject(!!v)}>
		{m.containers_group_by_project()}
	</DropdownMenu.CheckboxItem>
	<DropdownMenu.CheckboxItem checked={showInternal} onCheckedChange={(v) => setShowInternal(!!v)}>
		{`${m.common_show()} ${m.internal()} ${m.containers_title()}`}
	</DropdownMenu.CheckboxItem>
	<DropdownMenu.CheckboxItem
		checked={hideExposedPorts}
		onCheckedChange={(v) => {
			customSettings = { ...customSettings, hideExposedPorts: !!v };
		}}
	>
		{m.containers_hide_unexposed_ports()}
	</DropdownMenu.CheckboxItem>
{/snippet}

{#if killTarget}
	<KillContainerDialog
		containerId={killTarget.id}
		containerName={getContainerDisplayName(killTarget)}
		onClose={() => (killTarget = null)}
		onComplete={async () => {
			await refreshContainers(requestOptions);
		}}
	/>
{/if}
