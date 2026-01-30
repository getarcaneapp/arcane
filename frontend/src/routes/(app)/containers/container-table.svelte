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
		InspectIcon
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

	let isBulkLoading = $state({
		start: false,
		stop: false,
		restart: false,
		remove: false
	});

	let statsManager = $state<ContainerStatsManager | null>(null);

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

	function getContainerDisplayName(container: ContainerSummaryDto): string {
		if (container.names && container.names.length > 0) {
			return container.names[0].replace(/^\//, '');
		}
		return container.id.substring(0, 12);
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

	async function refreshContainers(options: SearchPaginationSortRequest) {
		const result = await containerService.getContainers(options);
		containers = result;
		return result;
	}

	function getCurrentLimit() {
		return requestOptions?.pagination?.limit ?? containers?.pagination?.itemsPerPage ?? 20;
	}

	function setShowInternal(value: boolean) {
		customSettings = { ...customSettings, showInternalContainers: value };
		const nextOptions: SearchPaginationSortRequest = {
			...requestOptions,
			includeInternal: value,
			pagination: { page: 1, limit: getCurrentLimit() }
		};
		requestOptions = nextOptions;
		refreshContainers(nextOptions);
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

	async function handleRemoveContainer(id: string, name: string) {
		openConfirmDialog({
			title: m.containers_remove_confirm_title(),
			message: m.containers_remove_confirm_message({ resource: name }),
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
		const containerName = getContainerDisplayName(container);

		openConfirmDialog({
			title: m.containers_update_confirm_title(),
			message: m.containers_update_confirm_message({ name: containerName }),
			confirm: {
				label: m.containers_update_container(),
				destructive: false,
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

	async function handleBulkStart(ids: string[]) {
		if (!ids || ids.length === 0) return;

		openConfirmDialog({
			title: m.containers_bulk_start_confirm_title({ count: ids.length }),
			message: m.containers_bulk_start_confirm_message({ count: ids.length }),
			confirm: {
				label: m.common_start(),
				destructive: false,
				action: async () => {
					isBulkLoading.start = true;

					const results = await Promise.allSettled(ids.map((id) => containerService.startContainer(id)));

					const successCount = results.filter((r) => r.status === 'fulfilled').length;
					const failureCount = results.length - successCount;

					isBulkLoading.start = false;

					if (successCount === ids.length) {
						toast.success(m.containers_bulk_start_success({ count: successCount }));
					} else if (successCount > 0) {
						toast.warning(m.containers_bulk_start_partial({ success: successCount, total: ids.length, failed: failureCount }));
					} else {
						toast.error(m.containers_start_failed());
					}

					containers = await containerService.getContainers(requestOptions);
					selectedIds = [];
				}
			}
		});
	}

	async function handleBulkStop(ids: string[]) {
		if (!ids || ids.length === 0) return;

		openConfirmDialog({
			title: m.containers_bulk_stop_confirm_title({ count: ids.length }),
			message: m.containers_bulk_stop_confirm_message({ count: ids.length }),
			confirm: {
				label: m.common_stop(),
				destructive: false,
				action: async () => {
					isBulkLoading.stop = true;

					const results = await Promise.allSettled(ids.map((id) => containerService.stopContainer(id)));

					const successCount = results.filter((r) => r.status === 'fulfilled').length;
					const failureCount = results.length - successCount;

					isBulkLoading.stop = false;

					if (successCount === ids.length) {
						toast.success(m.containers_bulk_stop_success({ count: successCount }));
					} else if (successCount > 0) {
						toast.warning(m.containers_bulk_stop_partial({ success: successCount, total: ids.length, failed: failureCount }));
					} else {
						toast.error(m.containers_stop_failed());
					}

					containers = await containerService.getContainers(requestOptions);
					selectedIds = [];
				}
			}
		});
	}

	async function handleBulkRestart(ids: string[]) {
		if (!ids || ids.length === 0) return;

		openConfirmDialog({
			title: m.containers_bulk_restart_confirm_title({ count: ids.length }),
			message: m.containers_bulk_restart_confirm_message({ count: ids.length }),
			confirm: {
				label: m.common_restart(),
				destructive: false,
				action: async () => {
					isBulkLoading.restart = true;

					const results = await Promise.allSettled(ids.map((id) => containerService.restartContainer(id)));

					const successCount = results.filter((r) => r.status === 'fulfilled').length;
					const failureCount = results.length - successCount;

					isBulkLoading.restart = false;

					if (successCount === ids.length) {
						toast.success(m.containers_bulk_restart_success({ count: successCount }));
					} else if (successCount > 0) {
						toast.warning(m.containers_bulk_restart_partial({ success: successCount, total: ids.length, failed: failureCount }));
					} else {
						toast.error(m.containers_restart_failed());
					}

					containers = await containerService.getContainers(requestOptions);
					selectedIds = [];
				}
			}
		});
	}

	async function handleBulkRemove(ids: string[]) {
		if (!ids || ids.length === 0) return;

		openConfirmDialog({
			title: m.containers_bulk_remove_confirm_title({ count: ids.length }),
			message: m.containers_bulk_remove_confirm_message({ count: ids.length }),
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
					isBulkLoading.remove = true;

					const results = await Promise.allSettled(ids.map((id) => containerService.deleteContainer(id, { force, volumes })));

					const successCount = results.filter((r) => r.status === 'fulfilled').length;
					const failureCount = results.length - successCount;

					isBulkLoading.remove = false;

					if (successCount === ids.length) {
						toast.success(m.containers_bulk_remove_success({ count: successCount }));
					} else if (successCount > 0) {
						toast.warning(m.containers_bulk_remove_partial({ success: successCount, total: ids.length, failed: failureCount }));
					} else {
						toast.error(m.containers_remove_failed());
					}

					containers = await containerService.getContainers(requestOptions);
					selectedIds = [];
				}
			}
		});
	}

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

	onMount(() => {
		collapsedGroupsState = new PersistedState<Record<string, boolean>>('container-groups-collapsed', {});

		statsManager = new ContainerStatsManager();

		// Derive which containers should be connected based on current state
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

		// Effect ONLY for side effects - connecting/disconnecting websockets
		const unsubscribe = $effect.root(() => {
			$effect(() => {
				if (!statsManager) return;

				const targetIds = shouldConnect;
				const connectedIds = new Set(statsManager.getConnectedIds());

				// Connect new containers
				for (const id of targetIds) {
					if (!connectedIds.has(id)) {
						statsManager.connect(id, currentEnvId);
					}
				}

				// Disconnect removed containers
				for (const id of connectedIds) {
					if (!targetIds.has(id)) {
						statsManager.disconnect(id);
					}
				}
			});

			return () => {};
		});

		return () => {
			unsubscribe();
			statsManager?.destroy();
		};
	});

	let groupByProject = $derived.by(() => {
		return (customSettings.groupByProject as boolean) ?? false;
	});

	function setGroupByProject(value: boolean) {
		customSettings = { ...customSettings, groupByProject: value };
	}

	$effect(() => {
		const current = requestOptions?.includeInternal;
		if (showInternal && current !== true) {
			setShowInternal(true);
		} else if (!showInternal && current === true) {
			setShowInternal(false);
		}
	});

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
			accessorFn: (row) => statsManager?.getCPUPercent(row.id) ?? -1,
			id: 'cpuUsage',
			title: m.containers_cpu_usage(),
			sortable: false,
			cell: CPUCell
		},
		{
			accessorFn: (row) => statsManager?.getMemoryPercent(row.id) ?? -1,
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

	function getProjectName(container: ContainerSummaryDto): string {
		const projectLabel = container.labels?.['com.docker.compose.project'];
		return projectLabel || 'No Project';
	}

	// Group by function for containers
	function groupContainerByProject(container: ContainerSummaryDto): string {
		return getProjectName(container);
	}

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
		value={statsManager?.getCPUPercent(item.id)}
		loading={statsManager?.isLoading(item.id) ?? false}
		stopped={item.state !== 'running'}
		type="cpu"
	/>
{/snippet}

{#snippet MemoryCell({ item }: { item: ContainerSummaryDto })}
	{@const memoryData = statsManager?.getMemoryUsage(item.id)}
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

{#snippet MobileRowActions({ item }: { item: ContainerSummaryDto })}
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
			},
			{
				label: m.containers_cpu_usage(),
				getValue: (item: ContainerSummaryDto) => {
					const cpu = statsManager?.getCPUPercent(item.id);
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
					const memData = statsManager?.getMemoryUsage(item.id);
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
		rowActions={MobileRowActions}
		onclick={(item: ContainerSummaryDto) => goto(`/containers/${item.id}`)}
	>
		{#snippet children()}
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
		{/snippet}
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
