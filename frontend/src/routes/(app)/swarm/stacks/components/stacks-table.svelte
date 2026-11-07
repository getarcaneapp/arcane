<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { LayersIcon, InspectIcon, TrashIcon, StopIcon, PlayIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { SwarmStackSummary } from '#lib/types/swarm.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import type { SwarmStacksPaginatedResponse } from '#lib/types/ui/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		stacks,
		scope,
		requestOptions = $bindable(),
		onRetry
	}: {
		stacks: StackPage;
		scope: string;
		requestOptions: SearchPaginationSortRequest;
		onRetry: () => Promise<unknown>;
	} = $props();

	const queryClient = useQueryClient();

	type StackRow = EnvironmentRow<SwarmStackSummary>;
	type StackPage = EnvironmentPage<SwarmStacksPaginatedResponse>;

	let isLoading = $state(false);

	function inspectStack(stack: StackRow) {
		goto(
			resolve(`/(app)/swarm/stacks/[name]?env=${encodeURIComponent(stack.environmentId)}`, {
				name: encodeURIComponent(stack.name)
			})
		);
	}

	function handleDelete(stack: StackRow) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_stack() }),
			message: m.common_delete_confirm({ resource: m.swarm_stack() }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.removeStack(stack.name, stack.environmentId)),
						message: m.common_delete_failed({ resource: `${m.swarm_stack()} "${stack.name}"` }),
						setLoadingState: (v) => (isLoading = v),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.swarm_stack()} "${stack.name}"` }));
							await queryClient.invalidateQueries({ queryKey: queryKeys.swarm.listPrefix(scope, 'stacks') });
						}
					});
				}
			}
		});
	}

	async function handleUp(stack: StackRow) {
		isLoading = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.upStack(stack.name, stack.environmentId)),
			message: m.failed_to_bring_up_stack({ name: stack.name }),
			setLoadingState: (v) => (isLoading = v),
			onSuccess: async () => {
				toast.success(m.stack_brought_up({ name: stack.name }));
				await queryClient.invalidateQueries({ queryKey: queryKeys.swarm.listPrefix(scope, 'stacks') });
			}
		});
	}

	function handleDown(stack: StackRow) {
		openConfirmDialog({
			title: m.bring_down_stack(),
			message: m.bring_down_stack_confirm_message(),
			confirm: {
				label: m.common_down(),
				destructive: true,
				action: async () => {
					isLoading = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.downStack(stack.name, stack.environmentId)),
						message: m.failed_to_bring_down_stack({ name: stack.name }),
						setLoadingState: (v) => (isLoading = v),
						onSuccess: async () => {
							toast.success(m.stack_brought_down({ name: stack.name }));
							await queryClient.invalidateQueries({ queryKey: queryKeys.swarm.listPrefix(scope, 'stacks') });
						}
					});
				}
			}
		});
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		...environmentColumns<StackRow>(),
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'services', title: m.services(), sortable: true },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'updatedAt', title: m.common_updated(), sortable: true, cell: UpdatedCell }
	] satisfies ColumnSpec<StackRow>[];

	const mobileFields = [
		{ id: 'services', label: m.services(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true },
		{ id: 'updatedAt', label: m.common_updated(), defaultVisible: false }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: StackRow })}
	<a
		href={resolve(`/(app)/swarm/stacks/[name]?env=${encodeURIComponent(item.environmentId)}`, {
			name: encodeURIComponent(item.name)
		})}
		class="text-sm font-medium text-primary-text hover:underline"
	>
		{item.name}
	</a>
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	<span class="text-sm">{formatRelativeTime(String(value ?? '')) || m.common_unknown()}</span>
{/snippet}

{#snippet UpdatedCell({ value }: { value: unknown })}
	<span class="text-sm">{formatRelativeTime(String(value ?? '')) || m.common_unknown()}</span>
{/snippet}

{#snippet StackMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: StackRow;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: LayersIcon,
			variant: 'purple'
		})}
		title={(item: StackRow) => item.name}
		subtitle={(item: StackRow) =>
			(mobileFieldVisibility['createdAt'] ?? true) ? formatRelativeTime(item.createdAt) || m.common_unknown() : null}
		fields={[
			{
				label: m.services(),
				getValue: (item: StackRow) => String(item.services),
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['services'] ?? true
			},
			{
				label: m.common_updated(),
				getValue: (item: StackRow) => formatRelativeTime(item.updatedAt) || m.common_unknown(),
				icon: LayersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['updatedAt'] ?? false
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: StackRow })}
	<RowActionsMenu name={item.name} triggerClass="relative size-8 p-0" iconClass="">
		<DropdownMenu.Item onclick={() => inspectStack(item)}>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>
		<IfPermitted perm="swarm-stacks:update" envId={item.environmentId}>
			{#if item.services > 0}
				<DropdownMenu.Item onclick={() => handleDown(item)} disabled={isLoading}>
					<StopIcon class="size-4" />
					{m.common_down()}
				</DropdownMenu.Item>
			{:else if item.hasSource}
				<DropdownMenu.Item onclick={() => handleUp(item)} disabled={isLoading}>
					<PlayIcon class="size-4" />
					{m.common_up()}
				</DropdownMenu.Item>
			{/if}
		</IfPermitted>
		<IfPermitted perm="swarm-stacks:delete" envId={item.environmentId}>
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => handleDelete(item)} disabled={isLoading}>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-swarm-stacks-table"
	items={stacks}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	{onRetry}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={StackMobileCardSnippet}
/>
