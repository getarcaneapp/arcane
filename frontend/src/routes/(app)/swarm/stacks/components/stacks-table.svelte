<script lang="ts">
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { LayersIcon, InspectIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService, type SwarmStacksPaginatedResponse } from '#lib/services/swarm-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { SwarmStackSummary } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { goto } from '#lib/utils/base-path.js';
	import { environmentHref, type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		stacks = $bindable(),
		requestOptions = $bindable(),
		fetchStacks
	}: {
		stacks: StackPage;
		requestOptions: SearchPaginationSortRequest;
		fetchStacks: (options: SearchPaginationSortRequest) => Promise<StackPage>;
	} = $props();

	type StackRow = EnvironmentRow<SwarmStackSummary>;
	type StackPage = EnvironmentPage<SwarmStacksPaginatedResponse>;

	let isLoading = $state(false);

	function inspectStack(stack: StackRow) {
		goto(environmentHref(`/swarm/stacks/${encodeURIComponent(stack.name)}`, stack.environmentId));
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
							stacks = await fetchStacks(requestOptions);
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
		href={environmentHref(`/swarm/stacks/${encodeURIComponent(item.name)}`, item.environmentId)}
		class="text-sm font-medium text-primary hover:underline"
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
	<RowActionsMenu triggerClass="relative size-8 p-0" iconClass="">
		<DropdownMenu.Item onclick={() => inspectStack(item)}>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>
		<DropdownMenu.Separator />
		<IfPermitted perm="swarm:stacks" envId={item.environmentId}>
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
	onRefresh={async (options) => (stacks = await fetchStacks(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={StackMobileCardSnippet}
/>
