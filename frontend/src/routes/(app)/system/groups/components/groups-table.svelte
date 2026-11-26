<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import type { BulkAction, ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { EditIcon, ShieldAlertIcon, TrashIcon, UsersIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { groupService } from '#lib/services/group-service.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { Paginated } from '#lib/types/base.js';
	import type { Group } from '#lib/types/group.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { bulkConfirmAndRun, confirmAndRun } from '#lib/utils/bulk-actions.js';

	let {
		groups,
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onRetry
	}: {
		groups: Paginated<Group>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onRetry: () => Promise<unknown>;
	} = $props();

	const queryClient = useQueryClient();
	const isAdmin = $derived(userStore.isGlobalAdmin());

	const isLoading = $state({
		removing: false
	});

	function handleDeleteSelected() {
		if (!selectedIds || selectedIds.length === 0) return;
		const ids = [...selectedIds];

		bulkConfirmAndRun({
			ids,
			title: m.delete_selected_groups_title({ count: ids.length }),
			message: m.delete_group_message({ name: m.common_unknown() }),
			confirmLabel: m.common_delete(),
			destructive: true,
			run: (groupId) => groupService.delete(groupId),
			messages: {
				success: (count) => m.common_bulk_delete_success({ count, resource: m.groups() }),
				partial: (success, total, failed) => m.common_bulk_delete_partial({ success, total, failed, resource: m.groups() }),
				failure: () => m.common_bulk_delete_failed({ count: ids.length, resource: m.groups() })
			},
			setLoading: (loading) => (isLoading.removing = loading),
			onComplete: async ({ success }) => {
				if (success > 0) await queryClient.invalidateQueries({ queryKey: queryKeys.groups.root });
			},
			clearSelection: () => (selectedIds = []),
			sequential: true
		});
	}

	function handleDeleteGroup(group: Group) {
		const safeName = group.name?.trim() || m.common_unknown();
		confirmAndRun({
			title: m.delete_group_title({ name: safeName }),
			message: m.delete_group_message({ name: safeName }),
			confirmLabel: m.common_delete(),
			destructive: true,
			setLoading: (loading) => (isLoading.removing = loading),
			run: () => groupService.delete(group.id),
			failureMessage: m.common_delete_failed({ resource: `${m.group()} "${safeName}"` }),
			onSuccess: async () => {
				toast.success(m.common_delete_success({ resource: `${m.group()} "${safeName}"` }));
				await queryClient.invalidateQueries({ queryKey: queryKeys.groups.root });
			}
		});
	}

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'description', title: m.common_description(), sortable: false, cell: DescriptionCell },
		{ id: 'members', accessorKey: 'memberCount', title: m.group_members(), sortable: false, cell: MembersCell },
		{
			id: 'roleAssignments',
			accessorKey: 'roleAssignmentCount',
			title: m.group_role_assignments(),
			sortable: false,
			cell: RoleAssignmentsCell
		}
	] satisfies ColumnSpec<Group>[];

	const mobileFields = [
		{ id: 'description', label: m.common_description(), defaultVisible: true },
		{ id: 'members', label: m.group_members(), defaultVisible: true },
		{ id: 'roleAssignments', label: m.group_role_assignments(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onclick: handleDeleteSelected,
			loading: isLoading.removing,
			disabled: isLoading.removing || (selectedIds?.length ?? 0) === 0 || !isAdmin,
			icon: TrashIcon
		}
	]);

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: Group })}
	<div class="flex items-center gap-2">
		<span class="font-medium">{item.name}</span>
	</div>
{/snippet}

{#snippet DescriptionCell({ item }: { item: Group })}
	{#if item.description}
		<span class="text-sm">{item.description}</span>
	{:else}
		<span class="text-muted-foreground">-</span>
	{/if}
{/snippet}

{#snippet MembersCell({ item }: { item: Group })}
	<span class="tabular-nums">{m.members_count({ count: item.memberCount })}</span>
{/snippet}

{#snippet RoleAssignmentsCell({ item }: { item: Group })}
	<span class="tabular-nums">{m.role_assignments_count({ count: item.roleAssignmentCount })}</span>
{/snippet}

{#snippet GroupMobileCardSnippet({ item, mobileFieldVisibility }: { item: Group; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: UsersIcon, variant: 'blue' }}
		title={(item: Group) => item.name}
		subtitle={(item: Group) => ((mobileFieldVisibility['description'] ?? true) && item.description ? item.description : null)}
		fields={[
			{
				label: m.group_members(),
				getValue: (item: Group) => m.members_count({ count: item.memberCount }),
				icon: UsersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['members'] ?? true
			},
			{
				label: m.group_role_assignments(),
				getValue: (item: Group) => m.role_assignments_count({ count: item.roleAssignmentCount }),
				icon: ShieldAlertIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['roleAssignments'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: Group })}
	{#if isAdmin}
		<RowActionsMenu name={item.name}>
			<IfPermitted adminOnly>
				<DropdownMenu.Item onclick={() => goto(resolve('/(app)/system/groups/[id]', { id: item.id }))}>
					<EditIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>

				<DropdownMenu.Separator />

				<DropdownMenu.Item variant="destructive" disabled={isLoading.removing} onclick={() => handleDeleteGroup(item)}>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</IfPermitted>
		</RowActionsMenu>
	{/if}
{/snippet}

<ArcaneTable
	persistKey="arcane-groups-table"
	items={groups}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	{bulkActions}
	{onRetry}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={GroupMobileCardSnippet}
/>
