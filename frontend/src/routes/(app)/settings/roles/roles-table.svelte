<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import { goto } from '$app/navigation';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import type { Role } from '$lib/types/auth';
	import { BUILT_IN_ROLE_ADMIN, BUILT_IN_ROLE_EDITOR, BUILT_IN_ROLE_DEPLOYER, BUILT_IN_ROLE_VIEWER } from '$lib/types/auth';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { roleService } from '$lib/services/role-service';
	import { ShieldAlertIcon, TrashIcon, EditIcon, EllipsisIcon } from '$lib/icons';
	import userStore from '$lib/stores/user-store';
	import IfPermitted from '$lib/components/if-permitted.svelte';

	let {
		roles = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onRolesChanged
	}: {
		roles: Paginated<Role>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onRolesChanged: () => Promise<void>;
	} = $props();

	const isAdmin = $derived(userStore.isGlobalAdmin());

	let isLoading = $state({
		removing: false
	});

	type BadgeVariant = 'red' | 'blue' | 'purple' | 'gray' | 'green' | 'amber';
	type IconVariant = 'emerald' | 'red' | 'amber' | 'blue' | 'purple' | 'gray' | 'sky' | 'orange';

	function getRoleBadgeVariant(role: Role): BadgeVariant {
		if (!role.builtIn) return 'green';
		switch (role.id) {
			case BUILT_IN_ROLE_ADMIN:
				return 'red';
			case BUILT_IN_ROLE_EDITOR:
				return 'blue';
			case BUILT_IN_ROLE_DEPLOYER:
				return 'purple';
			case BUILT_IN_ROLE_VIEWER:
				return 'gray';
			default:
				return 'green';
		}
	}

	function getRoleIconVariant(role: Role): IconVariant {
		const v = getRoleBadgeVariant(role);
		return v === 'green' ? 'emerald' : v;
	}

	const selectedRoles = $derived.by(() =>
		(selectedIds ?? []).map((id) => roles.data.find((role) => role.id === id)).filter((role): role is Role => Boolean(role))
	);

	const deletableSelectedIds = $derived.by(() => selectedRoles.filter((role) => !role.builtIn).map((role) => role.id));
	const hasBuiltInSelection = $derived.by(() => selectedRoles.some((role) => role.builtIn));

	async function handleDeleteSelected() {
		if (deletableSelectedIds.length === 0) return;

		openConfirmDialog({
			// TODO: i18n — add roles_delete_selected_title key
			title: `Delete ${deletableSelectedIds.length} role${deletableSelectedIds.length === 1 ? '' : 's'}?`,
			message: m.roles_delete_message({ name: m.common_unknown() }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					let successCount = 0;
					let failureCount = 0;

					for (const roleId of deletableSelectedIds) {
						const result = await tryCatch(roleService.delete(roleId));
						handleApiResultWithCallbacks({
							result,
							message: m.common_delete_failed({ resource: `${m.resource_role()} "${roleId}"` }),
							setLoadingState: () => {},
							onSuccess: () => {
								successCount++;
							}
						});

						if (result.error) {
							failureCount++;
						}
					}

					isLoading.removing = false;

					if (successCount > 0) {
						toast.success(m.common_bulk_delete_success({ count: successCount, resource: m.roles_title() }));
						await onRolesChanged();
					}

					if (failureCount > 0) {
						toast.error(m.common_bulk_delete_failed({ count: failureCount, resource: m.roles_title() }));
					}

					selectedIds = [];
				}
			}
		});
	}

	async function handleDeleteRole(role: Role) {
		if (role.builtIn) return;

		const safeName = role.name?.trim() || m.common_unknown();
		openConfirmDialog({
			// TODO: i18n — add roles_delete_title key
			title: `Delete role "${safeName}"?`,
			message: m.roles_delete_message({ name: safeName }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(roleService.delete(role.id)),
						message: m.common_delete_failed({ resource: `${m.resource_role()} "${safeName}"` }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: `${m.resource_role()} "${safeName}"` }));
							await onRolesChanged();
						}
					});
				}
			}
		});
	}

	const columns = [
		// TODO: i18n — add roles_name_label and roles_description_label keys
		{ accessorKey: 'name', title: 'Name', sortable: true, cell: NameCell },
		{ accessorKey: 'description', title: 'Description', sortable: false, cell: DescriptionCell },
		{ id: 'type', accessorKey: 'builtIn', title: m.roles_col_type(), sortable: false, cell: TypeCell },
		{
			id: 'assignedUsers',
			accessorKey: 'assignedUserCount',
			title: m.roles_col_assigned_users(),
			sortable: true,
			cell: AssignedUsersCell
		},
		{
			id: 'permissions',
			accessorKey: 'permissions',
			title: m.roles_col_permissions(),
			sortable: false,
			cell: PermissionsCell
		}
	] satisfies ColumnSpec<Role>[];

	const mobileFields = [
		// TODO: i18n — add roles_description_label key
		{ id: 'description', label: 'Description', defaultVisible: true },
		{ id: 'permissions', label: m.roles_col_permissions(), defaultVisible: true },
		{ id: 'assignedUsers', label: m.roles_col_assigned_users(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleDeleteSelected,
			loading: isLoading.removing,
			disabled: isLoading.removing || deletableSelectedIds.length === 0 || hasBuiltInSelection || !isAdmin,
			icon: TrashIcon
		}
	]);

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: Role })}
	<div class="flex items-center gap-2">
		<span class="font-medium">{item.name}</span>
	</div>
{/snippet}

{#snippet DescriptionCell({ item }: { item: Role })}
	{#if item.description}
		<span class="text-sm">{item.description}</span>
	{:else}
		<span class="text-muted-foreground">-</span>
	{/if}
{/snippet}

{#snippet TypeCell({ item }: { item: Role })}
	<StatusBadge text={item.builtIn ? m.roles_built_in() : m.roles_custom()} variant={item.builtIn ? 'amber' : 'blue'} />
{/snippet}

{#snippet AssignedUsersCell({ item }: { item: Role })}
	<span class="tabular-nums">{item.assignedUserCount === 1 ? '1 user' : `${item.assignedUserCount} users`}</span>
{/snippet}

{#snippet PermissionsCell({ item }: { item: Role })}
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger>
			<span class="tabular-nums underline decoration-dotted underline-offset-4">{item.permissions.length}</span>
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content>
			<p class="max-w-xs text-xs break-words">{item.permissions.join(', ')}</p>
		</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{/snippet}

{#snippet RoleMobileCardSnippet({ item, mobileFieldVisibility }: { item: Role; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: ShieldAlertIcon, variant: getRoleIconVariant(item) }}
		title={(item: Role) => item.name}
		subtitle={(item: Role) => ((mobileFieldVisibility['description'] ?? true) && item.description ? item.description : null)}
		badges={[
			(item: Role) => ({
				variant: item.builtIn ? 'amber' : 'blue',
				text: item.builtIn ? m.roles_built_in() : m.roles_custom()
			})
		]}
		fields={[
			{
				label: m.roles_col_permissions(),
				getValue: (item: Role) => String(item.permissions.length),
				icon: ShieldAlertIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['permissions'] ?? true
			},
			{
				label: m.roles_col_assigned_users(),
				// TODO: i18n — roles_assigned_users_count has a malformed ICU plural in en.json
				getValue: (item: Role) => (item.assignedUserCount === 1 ? '1 user' : `${item.assignedUserCount} users`),
				icon: ShieldAlertIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['assignedUsers'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: Role })}
	{#if isAdmin}
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
					<IfPermitted adminOnly>
						<DropdownMenu.Item onclick={() => goto(`/settings/roles/${item.id}`)}>
							<EditIcon class="size-4" />
							{m.common_edit()}
						</DropdownMenu.Item>

						<DropdownMenu.Separator />

						<DropdownMenu.Item
							variant="destructive"
							disabled={item.builtIn || isLoading.removing}
							onclick={() => handleDeleteRole(item)}
						>
							<TrashIcon class="size-4" />
							{m.common_delete()}
						</DropdownMenu.Item>
					</IfPermitted>
				</DropdownMenu.Group>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	{/if}
{/snippet}

<ArcaneTable
	persistKey="arcane-roles-table"
	items={roles}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	{bulkActions}
	onRefresh={async (options) => {
		requestOptions = options;
		await onRolesChanged();
		return roles;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={RoleMobileCardSnippet}
/>
