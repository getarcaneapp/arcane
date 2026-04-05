<script lang="ts">
	import { goto } from '$app/navigation';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { EditIcon, EllipsisIcon, LayersIcon, StartIcon, StopIcon, TrashIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { SwarmStackProjectRuntimeState, SwarmStackProjectSummary } from '$lib/types/swarm.type';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { format } from 'date-fns';
	import { toast } from 'svelte-sonner';

	let {
		stackProjects = $bindable(),
		requestOptions = $bindable(),
		onRefreshData
	}: {
		stackProjects: Paginated<SwarmStackProjectSummary>;
		requestOptions: SearchPaginationSortRequest;
		onRefreshData?: (options: SearchPaginationSortRequest) => Promise<Paginated<SwarmStackProjectSummary>>;
	} = $props();

	type ActionStatus = 'up' | 'down' | 'delete' | '';

	let actionStatus = $state<Record<string, ActionStatus>>({});
	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	const isAnyLoading = $derived(Object.values(actionStatus).some((status) => status !== ''));

	function setActionStatus(name: string, status: ActionStatus) {
		actionStatus = {
			...actionStatus,
			[name]: status
		};
	}

	function formatTimestamp(timestamp: string) {
		if (!timestamp) return m.common_na();
		return format(new Date(timestamp), 'PP p');
	}

	function runtimeStateVariant(state: SwarmStackProjectRuntimeState) {
		switch (state) {
			case 'live':
				return 'green';
			case 'down':
				return 'red';
			default:
				return 'amber';
		}
	}

	function runtimeStateLabel(state: SwarmStackProjectRuntimeState) {
		switch (state) {
			case 'live':
				return 'Live';
			case 'down':
				return 'Down';
			default:
				return 'Unavailable';
		}
	}

	async function refreshStackProjects(options: SearchPaginationSortRequest = requestOptions) {
		if (onRefreshData) {
			return onRefreshData(options);
		}

		stackProjects = await swarmService.getStackProjects(options);
		return stackProjects;
	}

	function openProject(item: SwarmStackProjectSummary) {
		goto(`/projects/swarm/${encodeURIComponent(item.name)}`);
	}

	async function handleUp(item: SwarmStackProjectSummary) {
		setActionStatus(item.name, 'up');

		try {
			const detail = await swarmService.getStackProject(item.name);
			await handleApiResultWithCallbacks({
				result: await tryCatch(
					swarmService.deployStack({
						name: item.name,
						composeContent: detail.composeContent,
						envContent: detail.envContent ?? ''
					})
				),
				message: m.common_action_failed(),
				setLoadingState: (value) => {
					setActionStatus(item.name, value ? 'up' : '');
				},
				onSuccess: async () => {
					toast.success(`${m.swarm_stack()} "${item.name}" is live.`);
					await refreshStackProjects();
				}
			});
		} finally {
			if (actionStatus[item.name] === 'up') {
				setActionStatus(item.name, '');
			}
		}
	}

	function handleDown(item: SwarmStackProjectSummary) {
		openConfirmDialog({
			title: `${m.common_down()} ${m.swarm_stack()}`,
			message: `Bring down the live runtime for "${item.name}" and keep the saved files?`,
			confirm: {
				label: m.common_down(),
				destructive: true,
				action: async () => {
					await handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.downStack(item.name)),
						message: m.common_action_failed(),
						setLoadingState: (value) => {
							setActionStatus(item.name, value ? 'down' : '');
						},
						onSuccess: async () => {
							toast.success(`${m.swarm_stack()} "${item.name}" was brought down.`);
							await refreshStackProjects();
						}
					});
				}
			}
		});
	}

	function handleDelete(item: SwarmStackProjectSummary) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_stack() }),
			message: `Delete the saved files for "${item.name}"?`,
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					await handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.deleteStackProject(item.name)),
						message: m.common_delete_failed({ resource: `${m.swarm_stack()} "${item.name}"` }),
						setLoadingState: (value) => {
							setActionStatus(item.name, value ? 'delete' : '');
						},
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.swarm_stack()} "${item.name}"` }));
							await refreshStackProjects();
						}
					});
				}
			}
		});
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'runtimeState', title: m.common_status(), sortable: true, cell: RuntimeStateCell },
		{ accessorKey: 'serviceCount', title: m.services(), sortable: true },
		{ accessorKey: 'updatedAt', title: m.common_updated(), sortable: true, cell: UpdatedCell }
	] satisfies ColumnSpec<SwarmStackProjectSummary>[];

	const mobileFields = [
		{ id: 'runtimeState', label: m.common_status(), defaultVisible: true },
		{ id: 'serviceCount', label: m.services(), defaultVisible: true },
		{ id: 'updatedAt', label: m.common_updated(), defaultVisible: true }
	];
</script>

{#snippet NameCell({ item }: { item: SwarmStackProjectSummary })}
	<a href="/projects/swarm/{encodeURIComponent(item.name)}" class="text-primary text-sm font-medium hover:underline">
		{item.name}
	</a>
{/snippet}

{#snippet RuntimeStateCell({ value }: { value: unknown })}
	<StatusBadge
		text={runtimeStateLabel((value as SwarmStackProjectRuntimeState | undefined) ?? 'unavailable')}
		variant={runtimeStateVariant((value as SwarmStackProjectRuntimeState | undefined) ?? 'unavailable')}
	/>
{/snippet}

{#snippet UpdatedCell({ value }: { value: unknown })}
	<span class="text-sm">{formatTimestamp(String(value ?? ''))}</span>
{/snippet}

{#snippet SwarmProjectMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: SwarmStackProjectSummary;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: LayersIcon,
			variant: item.runtimeState === 'live' ? 'emerald' : item.runtimeState === 'down' ? 'red' : 'amber'
		})}
		title={(item: SwarmStackProjectSummary) => item.name}
		badges={[
			(item: SwarmStackProjectSummary) =>
				(mobileFieldVisibility['runtimeState'] ?? true)
					? {
							variant: runtimeStateVariant(item.runtimeState),
							text: runtimeStateLabel(item.runtimeState)
						}
					: null
		]}
		fields={[
			{
				label: m.services(),
				getValue: (item: SwarmStackProjectSummary) => String(item.serviceCount),
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['serviceCount'] ?? true
			},
			{
				label: m.common_updated(),
				getValue: (item: SwarmStackProjectSummary) => formatTimestamp(item.updatedAt),
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['updatedAt'] ?? true
			}
		]}
		rowActions={RowActions}
		onclick={() => openProject(item)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SwarmStackProjectSummary })}
	{@const status = actionStatus[item.name]}
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
				<DropdownMenu.Item onclick={() => openProject(item)} disabled={isAnyLoading}>
					<EditIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>
				{#if item.runtimeState === 'live'}
					<DropdownMenu.Item onclick={() => handleDown(item)} disabled={status === 'down' || isAnyLoading}>
						{#if status === 'down'}
							<Spinner class="size-4" />
						{:else}
							<StopIcon class="size-4" />
						{/if}
						{m.common_down()}
					</DropdownMenu.Item>
				{:else}
					<DropdownMenu.Item onclick={() => handleUp(item)} disabled={status === 'up' || isAnyLoading}>
						{#if status === 'up'}
							<Spinner class="size-4" />
						{:else}
							<StartIcon class="size-4" />
						{/if}
						{m.common_up()}
					</DropdownMenu.Item>
				{/if}
				<DropdownMenu.Separator />
				<DropdownMenu.Item
					variant="destructive"
					onclick={() => handleDelete(item)}
					disabled={status === 'delete' || isAnyLoading || item.runtimeState === 'live'}
				>
					{#if status === 'delete'}
						<Spinner class="size-4" />
					{:else}
						<TrashIcon class="size-4" />
					{/if}
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-swarm-stack-projects-table"
	items={stackProjects}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={refreshStackProjects}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={SwarmProjectMobileCardSnippet}
/>
