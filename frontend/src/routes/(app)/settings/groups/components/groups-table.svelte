<script lang="ts">
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import type { BulkAction, ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { EditIcon, ShieldAlertIcon, TrashIcon, UsersIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { groupService } from '#lib/services/group-service.js';
	import type { Group } from '#lib/types/auth.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { bulkConfirmAndRun, confirmAndRun } from '#lib/utils/bulk-actions.js';

	let {
		groups = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onGroupsChanged
	}: {
		groups: Paginated<Group>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onGroupsChanged: () => Promise<void>;
	} = $props();

	let isLoading = $state({
		removing: false
	});

	function handleDeleteSelected() {
		if (selectedIds.length === 0) return;
		const ids = [...selectedIds];

		bulkConfirmAndRun({
			ids,
			title: m.groups_delete_selected_title({ count: ids.length }),
			message: m.groups_delete_message({ name: m.common_unknown() }),
			confirmLabel: m.common_delete(),
			destructive: true,
			run: (groupId) => groupService.delete(groupId),
			messages: {
				success: (count) => m.common_bulk_delete_success({ count, resource: m.groups_title() }),
				partial: (success, total, failed) => m.common_bulk_delete_partial({ success, total, failed, resource: m.groups_title() }),
				failure: () => m.common_bulk_delete_failed({ count: ids.length, resource: m.groups_title() })
			},
			setLoading: (loading) => (isLoading.removing = loading),
			onComplete: async ({ success }) => {
				if (success > 0) await onGroupsChanged();
			},
			clearSelection: () => (selectedIds = []),
			sequential: true
		});
	}

	function handleDeleteGroup(group: Group) {
		const safeName = group.name?.trim() || m.common_unknown();
		confirmAndRun({
			title: m.groups_delete_title({ name: safeName }),
			message: m.groups_delete_message({ name: safeName }),
			confirmLabel: m.common_delete(),
			destructive: true,
			setLoading: (loading) => (isLoading.removing = loading),
			run: () => groupService.delete(group.id),
			failureMessage: m.common_delete_failed({ resource: `${m.resource_group()} "${safeName}"` }),
			onSuccess: async () => {
				toast.success(m.common_delete_success({ resource: `${m.resource_group_cap()} "${safeName}"` }));
				await onGroupsChanged();
			}
		});
	}

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'description', title: m.common_description(), sortable: false, cell: DescriptionCell },
		{ id: 'members', accessorKey: 'memberCount', title: m.groups_col_members(), sortable: true, cell: MembersCell },
		{ id: 'bindings', accessorKey: 'bindingCount', title: m.bindings_title(), sortable: true, cell: BindingsCell }
	] satisfies ColumnSpec<Group>[];

	const mobileFields = [
		{ id: 'description', label: m.common_description(), defaultVisible: true },
		{ id: 'members', label: m.groups_col_members(), defaultVisible: true },
		{ id: 'bindings', label: m.bindings_title(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleDeleteSelected,
			loading: isLoading.removing,
			disabled: isLoading.removing || !hasPermission(PERMISSIONS.GROUPS_DELETE),
			icon: TrashIcon
		}
	]);

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: Group })}
	<a href={`/settings/groups/${item.id}`} class="font-medium hover:underline">{item.name}</a>
{/snippet}

{#snippet DescriptionCell({ item }: { item: Group })}
	{#if item.description}
		<span class="text-sm">{item.description}</span>
	{:else}
		<span class="text-muted-foreground">-</span>
	{/if}
{/snippet}

{#snippet MembersCell({ item }: { item: Group })}
	<span class="tabular-nums">{m.groups_member_count({ count: item.memberCount })}</span>
{/snippet}

{#snippet BindingsCell({ item }: { item: Group })}
	<span class="tabular-nums">{m.bindings_count({ count: item.bindingCount })}</span>
{/snippet}

{#snippet GroupMobileCardSnippet({ item, mobileFieldVisibility }: { item: Group; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: UsersIcon, variant: 'blue' }}
		title={(item: Group) => item.name}
		subtitle={(item: Group) => ((mobileFieldVisibility['description'] ?? true) && item.description ? item.description : null)}
		fields={[
			{
				label: m.groups_col_members(),
				getValue: (item: Group) => m.groups_member_count({ count: item.memberCount }),
				icon: UsersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['members'] ?? true
			},
			{
				label: m.bindings_title(),
				getValue: (item: Group) => m.bindings_count({ count: item.bindingCount }),
				icon: ShieldAlertIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['bindings'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: Group })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(`/settings/groups/${item.id}`)}>
			<EditIcon class="size-4" />
			{m.common_view_details()}
		</DropdownMenu.Item>

		<IfPermitted perm={PERMISSIONS.GROUPS_DELETE}>
			<DropdownMenu.Separator />

			<DropdownMenu.Item variant="destructive" disabled={isLoading.removing} onclick={() => handleDeleteGroup(item)}>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-groups-table"
	items={groups}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	{bulkActions}
	onRefresh={async (options) => {
		requestOptions = options;
		await onGroupsChanged();
		return groups;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={GroupMobileCardSnippet}
/>
