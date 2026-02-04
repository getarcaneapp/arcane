<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { PortBadge } from '$lib/components/badges/index.js';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { getStatusVariant } from '$lib/utils/status.utils';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import type { RuntimeService } from '$lib/types/project.type';
	import type { ColumnSpec, BulkAction } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { containerService } from '$lib/services/container-service';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import IconImage from '$lib/components/icon-image.svelte';
	import { getArcaneIconUrlFromLabels } from '$lib/utils/arcane-labels';
	import {
		StartIcon,
		StopIcon,
		RefreshIcon,
		TrashIcon,
		EllipsisIcon,
		ContainersIcon,
		HealthIcon,
		InspectIcon,
		FolderXIcon,
		BoxIcon
	} from '$lib/icons';

	interface Props {
		services?: RuntimeService[];
		projectId?: string;
		onRefresh?: () => Promise<void>;
	}

	let { services = [], projectId, onRefresh }: Props = $props();

	// Convert RuntimeService to a format compatible with ArcaneTable
	type ServiceWithId = RuntimeService & { id: string };

	const servicesWithIds = $derived<ServiceWithId[]>(
		(services ?? []).map((service) => ({
			...service,
			id: service.containerId || service.name
		}))
	);

	// Track action status per container ID
	type ActionStatus = 'starting' | 'stopping' | 'restarting' | 'removing' | '';
	let actionStatus = $state<Record<string, ActionStatus>>({});

	let isBulkLoading = $state({
		start: false,
		stop: false,
		restart: false,
		remove: false
	});

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	// Fake request options and pagination for the table (we're using local data)
	let requestOptions = $state({
		pagination: { page: 1, limit: 100 },
		sort: { column: 'name', direction: 'asc' as const }
	});

	const paginatedServices = $derived({
		data: servicesWithIds,
		pagination: {
			currentPage: 1,
			totalPages: 1,
			totalItems: servicesWithIds.length,
			itemsPerPage: 100
		}
	});

	function getHealthColor(health: string | undefined): 'green' | 'red' | 'amber' {
		if (!health) return 'amber';
		const normalized = health.toLowerCase();
		if (normalized === 'healthy') return 'green';
		if (normalized === 'unhealthy') return 'red';
		return 'amber';
	}

	function getContainerUrl(service: RuntimeService): string {
		if (!service.containerId) return '#';
		return projectId
			? `/containers/${service.containerId}?from=project&projectId=${projectId}`
			: `/containers/${service.containerId}`;
	}

	async function performContainerAction(action: 'start' | 'stop' | 'restart', id: string) {
		if (!id) return;

		const statusMap = { start: 'starting', stop: 'stopping', restart: 'restarting' } as const;
		actionStatus[id] = statusMap[action];

		try {
			const serviceCall =
				action === 'start'
					? containerService.startContainer(id)
					: action === 'stop'
						? containerService.stopContainer(id)
						: containerService.restartContainer(id);

			const messageMap = {
				start: { failed: m.containers_start_failed(), success: m.containers_start_success() },
				stop: { failed: m.containers_stop_failed(), success: m.containers_stop_success() },
				restart: { failed: m.containers_restart_failed(), success: m.containers_restart_success() }
			};

			handleApiResultWithCallbacks({
				result: await tryCatch(serviceCall),
				message: messageMap[action].failed,
				setLoadingState: (value) => {
					actionStatus[id] = value ? statusMap[action] : '';
				},
				async onSuccess() {
					toast.success(messageMap[action].success);
					await onRefresh?.();
				}
			});
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
				{ id: 'force', label: m.containers_remove_force_label(), initialState: false },
				{ id: 'volumes', label: m.containers_remove_volumes_label(), initialState: false }
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
							await onRefresh?.();
						}
					});
				}
			}
		});
	}

	async function handleBulkAction(action: 'start' | 'stop' | 'restart' | 'remove', ids: string[]) {
		if (!ids || ids.length === 0) return;

		const validIds = ids.filter((id) => servicesWithIds.find((s) => s.id === id)?.containerId);
		if (validIds.length === 0) return;

		const actionConfig = {
			start: {
				title: m.containers_bulk_start_confirm_title({ count: validIds.length }),
				message: m.containers_bulk_start_confirm_message({ count: validIds.length }),
				label: m.common_start(),
				destructive: false,
				fn: (id: string) => containerService.startContainer(id),
				success: m.containers_bulk_start_success({ count: validIds.length }),
				loadingKey: 'start' as const
			},
			stop: {
				title: m.containers_bulk_stop_confirm_title({ count: validIds.length }),
				message: m.containers_bulk_stop_confirm_message({ count: validIds.length }),
				label: m.common_stop(),
				destructive: false,
				fn: (id: string) => containerService.stopContainer(id),
				success: m.containers_bulk_stop_success({ count: validIds.length }),
				loadingKey: 'stop' as const
			},
			restart: {
				title: m.containers_bulk_restart_confirm_title({ count: validIds.length }),
				message: m.containers_bulk_restart_confirm_message({ count: validIds.length }),
				label: m.common_restart(),
				destructive: false,
				fn: (id: string) => containerService.restartContainer(id),
				success: m.containers_bulk_restart_success({ count: validIds.length }),
				loadingKey: 'restart' as const
			},
			remove: {
				title: m.containers_bulk_remove_confirm_title({ count: validIds.length }),
				message: m.containers_bulk_remove_confirm_message({ count: validIds.length }),
				label: m.common_remove(),
				destructive: true,
				fn: (id: string) => containerService.deleteContainer(id, { force: false, volumes: false }),
				success: m.containers_bulk_remove_success({ count: validIds.length }),
				loadingKey: 'remove' as const
			}
		};

		const config = actionConfig[action];

		openConfirmDialog({
			title: config.title,
			message: config.message,
			confirm: {
				label: config.label,
				destructive: config.destructive,
				action: async () => {
					isBulkLoading[config.loadingKey] = true;

					const results = await Promise.allSettled(validIds.map((id) => config.fn(id)));
					const successCount = results.filter((r) => r.status === 'fulfilled').length;

					isBulkLoading[config.loadingKey] = false;

					if (successCount === validIds.length) {
						toast.success(config.success);
					} else if (successCount > 0) {
						toast.warning(`${successCount} of ${validIds.length} succeeded`);
					} else {
						toast.error(m.containers_action_error());
					}

					selectedIds = [];
					await onRefresh?.();
				}
			}
		});
	}

	const isAnyLoading = $derived(
		Object.values(actionStatus).some((status) => status !== '') || Object.values(isBulkLoading).some((loading) => loading)
	);

	const showActionsColumn = $derived(servicesWithIds.some((service) => service.status === 'running'));

	const columns = [
		{ accessorKey: 'containerName', id: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'status', title: m.common_state(), cell: StateCell },
		{ accessorKey: 'image', title: m.common_image() },
		{ accessorKey: 'health', title: m.common_health_status(), cell: HealthCell },
		{ accessorKey: 'ports', title: m.common_ports(), cell: PortsCell }
	] satisfies ColumnSpec<ServiceWithId>[];

	const mobileFields = [
		{ id: 'status', label: m.common_state(), defaultVisible: true },
		{ id: 'image', label: m.common_image(), defaultVisible: true },
		{ id: 'health', label: m.common_health_status(), defaultVisible: true },
		{ id: 'ports', label: m.common_ports(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'start',
			label: m.containers_bulk_start({ count: selectedIds?.length ?? 0 }),
			action: 'start',
			onClick: (ids) => handleBulkAction('start', ids),
			loading: isBulkLoading.start,
			disabled: isAnyLoading,
			icon: StartIcon
		},
		{
			id: 'stop',
			label: m.containers_bulk_stop({ count: selectedIds?.length ?? 0 }),
			action: 'stop',
			onClick: (ids) => handleBulkAction('stop', ids),
			loading: isBulkLoading.stop,
			disabled: isAnyLoading,
			icon: StopIcon
		},
		{
			id: 'restart',
			label: m.containers_bulk_restart({ count: selectedIds?.length ?? 0 }),
			action: 'restart',
			onClick: (ids) => handleBulkAction('restart', ids),
			loading: isBulkLoading.restart,
			disabled: isAnyLoading,
			icon: RefreshIcon
		},
		{
			id: 'remove',
			label: m.containers_bulk_remove({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: (ids) => handleBulkAction('remove', ids),
			loading: isBulkLoading.remove,
			disabled: isAnyLoading,
			icon: TrashIcon
		}
	]);
</script>

{#snippet NameCell({ item }: { item: ServiceWithId })}
	{@const displayName = item.containerName || item.name}
	{@const iconUrl = item.iconUrl ?? getArcaneIconUrlFromLabels(item.serviceConfig?.labels)}
	<div class="flex items-center gap-2">
		<IconImage src={iconUrl} alt={displayName} fallback={BoxIcon} class="size-4" containerClass="size-7" />
		{#if item.containerId}
			<a class="font-medium hover:underline" href={getContainerUrl(item)}>
				{displayName}
			</a>
		{:else}
			<span class="text-muted-foreground">{displayName}</span>
		{/if}
	</div>
{/snippet}

{#snippet StateCell({ item }: { item: ServiceWithId })}
	{@const status = actionStatus[item.id]}
	{#if status}
		<div class="flex items-center gap-1.5">
			<Spinner class="size-3.5" />
			<span class="text-muted-foreground text-xs font-medium">
				{status === 'starting'
					? m.common_action_starting()
					: status === 'stopping'
						? m.common_action_stopping()
						: status === 'restarting'
							? m.common_action_restarting()
							: m.common_action_removing()}
			</span>
		</div>
	{:else}
		<StatusBadge variant={getStatusVariant(item.status)} text={capitalizeFirstLetter(item.status)} />
	{/if}
{/snippet}

{#snippet HealthCell({ item }: { item: ServiceWithId })}
	{#if item.health}
		<div class="flex items-center gap-1.5">
			<HealthIcon class="size-4 text-{getHealthColor(item.health)}-500" />
			<span class="text-muted-foreground text-sm">{capitalizeFirstLetter(item.health)}</span>
		</div>
	{:else}
		<span class="text-muted-foreground text-sm">—</span>
	{/if}
{/snippet}

{#snippet PortsCell({ item }: { item: ServiceWithId })}
	{#if item.serviceConfig?.ports && item.serviceConfig.ports.length > 0}
		<PortBadge ports={item.serviceConfig.ports as any} />
	{:else if item.ports && item.ports.length > 0}
		{@const parsedPorts = item.ports.map((p) => {
			const [numsPart, proto] = p.split('/');
			const nums = numsPart.split(':');
			if (nums.length === 2) {
				return { publicPort: parseInt(nums[0]), privatePort: parseInt(nums[1]), type: proto || 'tcp' };
			}
			return { privatePort: parseInt(nums[0]), type: proto || 'tcp' };
		})}
		<PortBadge ports={parsedPorts} />
	{:else}
		<span class="text-muted-foreground text-sm">—</span>
	{/if}
{/snippet}

{#snippet ContainerMobileCard({
	item,
	mobileFieldVisibility
}: {
	row: any;
	item: ServiceWithId;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={(item) => {
			const iconUrl = item.iconUrl ?? getArcaneIconUrlFromLabels(item.serviceConfig?.labels);
			return {
				component: BoxIcon,
				variant: item.status === 'running' ? 'emerald' : item.status === 'exited' ? 'red' : 'amber',
				imageUrl: iconUrl ?? undefined,
				alt: item.containerName || item.name
			};
		}}
		title={(item) => item.containerName || item.name}
		badges={[
			(item) =>
				(mobileFieldVisibility.status ?? true)
					? {
							variant: item.status === 'running' ? 'green' : item.status === 'exited' ? 'red' : 'amber',
							text: capitalizeFirstLetter(item.status)
						}
					: null,
			(item) =>
				(mobileFieldVisibility.health ?? true) && item.health
					? { variant: getHealthColor(item.health), text: capitalizeFirstLetter(item.health) }
					: null
		]}
		fields={[
			{
				label: m.common_image(),
				getValue: (item: ServiceWithId) => item.image,
				show: mobileFieldVisibility.image ?? true
			}
		]}
		rowActions={showActionsColumn ? MobileRowActions : undefined}
		onclick={(item: ServiceWithId) => item.containerId && goto(getContainerUrl(item))}
	/>
{/snippet}

{#snippet MobileRowActions({ item }: { item: ServiceWithId })}
	{#if item.status === 'running'}
		{#if !item.containerId}
			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					<ArcaneButton action="base" tone="ghost" size="icon" class="size-8" disabled>
						<EllipsisIcon class="size-4" />
					</ArcaneButton>
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>
					<p>{m.compose_service_not_created()}</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{:else}
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
						<DropdownMenu.Item onclick={() => goto(getContainerUrl(item))} disabled={isAnyLoading}>
							<InspectIcon class="size-4" />
							{m.common_inspect()}
						</DropdownMenu.Item>
						<DropdownMenu.Separator />
						<DropdownMenu.Item
							variant="destructive"
							onclick={() => handleRemoveContainer(item.containerId!, item.containerName || item.name)}
							disabled={actionStatus[item.id] === 'removing' || isAnyLoading}
						>
							{#if actionStatus[item.id] === 'removing'}
								<Spinner class="size-4" />
							{:else}
								<TrashIcon class="size-4" />
							{/if}
							{m.common_remove()}
						</DropdownMenu.Item>
					</DropdownMenu.Group>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		{/if}
	{/if}
{/snippet}

{#snippet RowActions({ item }: { item: ServiceWithId })}
	{@const status = actionStatus[item.id]}

	{#if item.status === 'running'}
		{#if !item.containerId}
			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					<ArcaneButton action="base" tone="ghost" size="icon" class="size-8" disabled>
						<EllipsisIcon class="size-4" />
					</ArcaneButton>
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>
					<p>{m.compose_service_not_created()}</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{:else}
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-8">
							<span class="sr-only">{m.common_open_menu()}</span>
							{#if status}
								<Spinner class="size-4" />
							{:else}
								<EllipsisIcon class="size-4" />
							{/if}
						</ArcaneButton>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="end">
					<DropdownMenu.Group>
						<DropdownMenu.Item onclick={() => goto(getContainerUrl(item))} disabled={isAnyLoading}>
							<InspectIcon class="size-4" />
							{m.common_inspect()}
						</DropdownMenu.Item>

						<DropdownMenu.Separator />

						<DropdownMenu.Item
							onclick={() => performContainerAction('stop', item.containerId!)}
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
							onclick={() => performContainerAction('restart', item.containerId!)}
							disabled={status === 'restarting' || isAnyLoading}
						>
							{#if status === 'restarting'}
								<Spinner class="size-4" />
							{:else}
								<RefreshIcon class="size-4" />
							{/if}
							{m.common_restart()}
						</DropdownMenu.Item>

						<DropdownMenu.Separator />

						<DropdownMenu.Item
							variant="destructive"
							onclick={() => handleRemoveContainer(item.containerId!, item.containerName || item.name)}
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
		{/if}
	{/if}
{/snippet}

{#if servicesWithIds.length > 0}
	<ArcaneTable
		items={paginatedServices}
		bind:requestOptions
		bind:selectedIds
		bind:mobileFieldVisibility
		selectionDisabled
		onRefresh={async () => {
			await onRefresh?.();
			return paginatedServices;
		}}
		{columns}
		{mobileFields}
		{bulkActions}
		rowActions={showActionsColumn ? RowActions : undefined}
		mobileCard={ContainerMobileCard}
		withoutSearch
		withoutPagination
	/>
{:else}
	<div class="flex h-full items-center justify-center py-12">
		<Empty.Root class="bg-card/30 rounded-lg py-12 backdrop-blur-sm" role="status" aria-live="polite">
			<Empty.Header>
				<Empty.Media variant="icon">
					<FolderXIcon class="text-muted-foreground/40 size-10" />
				</Empty.Media>
				<Empty.Title class="text-base font-medium">{m.compose_no_services_found()}</Empty.Title>
			</Empty.Header>
		</Empty.Root>
	</div>
{/if}
