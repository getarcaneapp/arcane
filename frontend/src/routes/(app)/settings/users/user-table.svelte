<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import EditIcon from '@lucide/svelte/icons/edit';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { User } from '$lib/types/user.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { userService } from '$lib/services/user-service';
	import UserIcon from '@lucide/svelte/icons/user';

	let {
		users = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onUsersChanged,
		onEditUser
	}: {
		users: Paginated<User>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onUsersChanged: () => Promise<void>;
		onEditUser: (user: User) => void;
	} = $props();

	let isLoading = $state({
		removing: false
	});

	async function handleDeleteSelected() {
		if (selectedIds.length === 0) return;

		openConfirmDialog({
			title: m.users_delete_selected_title({ count: selectedIds.length }),
			message: m.users_delete_selected_message({ count: selectedIds.length }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					let successCount = 0;
					let failureCount = 0;

					for (const userId of selectedIds) {
						const result = await tryCatch(userService.delete(userId));
						handleApiResultWithCallbacks({
							result,
							message: m.users_delete_selected_item_failed({ id: userId }),
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
						const msg = m.common_bulk_delete_success({ count: successCount, resource: m.users_title() });
						toast.success(msg);
						await onUsersChanged();
					}

					if (failureCount > 0) {
						const msg = m.common_bulk_delete_failed({ count: failureCount, resource: m.users_title() });
						toast.error(msg);
					}

					selectedIds = [];
				}
			}
		});
	}

	async function handleDeleteUser(userId: string, username: string) {
		const safeName = username?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.users_delete_user_title({ username: safeName }),
			message: m.users_delete_user_message({ username: safeName }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(userService.delete(userId)),
						message: m.users_delete_user_failed({ username: safeName }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.users_delete_user_success({ username: safeName }));
							await onUsersChanged();
						}
					});
				}
			}
		});
	}

	function getRoleBadgeVariant(roles: string[]) {
		if (roles?.includes('admin')) return 'red';
		return 'green';
	}

	function getRoleText(roles: string[]) {
		if (roles?.includes('admin')) return m.common_admin();
		return m.common_user();
	}

	const columns = [
		{ accessorKey: 'username', title: m.common_username(), sortable: true, cell: UsernameCell },
		{ accessorKey: 'displayName', title: m.common_display_name(), sortable: true, cell: DisplayNameCell },
		{ accessorKey: 'email', title: m.common_email(), sortable: true, cell: EmailCell },
		{ accessorKey: 'roles', title: m.common_role(), sortable: true, cell: RoleCell }
	] satisfies ColumnSpec<User>[];

	const mobileFields = [
		{ id: 'displayName', label: m.common_display_name(), defaultVisible: true },
		{ id: 'email', label: m.common_email(), defaultVisible: true },
		{ id: 'roles', label: m.common_role(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet UsernameCell({ item }: { item: User })}
	<span class="font-medium">{item.username}</span>
{/snippet}

{#snippet DisplayNameCell({ value }: { value: unknown })}
	{String(value || '-')}
{/snippet}

{#snippet EmailCell({ value }: { value: unknown })}
	{String(value || '-')}
{/snippet}

{#snippet RoleCell({ item }: { item: User })}
	<StatusBadge text={getRoleText(item.roles)} variant={getRoleBadgeVariant(item.roles)} />
{/snippet}

{#snippet UserMobileCardSnippet({
	row,
	item,
	mobileFieldVisibility
}: {
	row: any;
	item: User;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: UserIcon, variant: 'blue' }}
		title={(item: User) => item.username}
		subtitle={(item: User) => ((mobileFieldVisibility.email ?? true) && item.email ? item.email : null)}
		badges={[
			(item: User) =>
				(mobileFieldVisibility.roles ?? true)
					? {
							variant: getRoleBadgeVariant(item.roles) === 'red' ? 'red' : 'green',
							text: getRoleText(item.roles)
						}
					: null
		]}
		fields={[
			{
				label: m.common_display_name(),
				getValue: (item: User) => item.displayName,
				icon: UserIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility.displayName ?? true) && !!item.displayName
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: User })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="ghost" size="icon" class="relative size-8 p-0">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon />
				</Button>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				{#if !item.oidcSubjectId}
					<DropdownMenu.Item onclick={() => onEditUser(item)}>
						<EditIcon class="size-4" />
						{m.common_edit()}
					</DropdownMenu.Item>
				{/if}
				<DropdownMenu.Item variant="destructive" onclick={() => handleDeleteUser(item.id, item.username)}>
					<Trash2Icon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-users-table"
	items={users}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRemoveSelected={(ids) => handleDeleteSelected()}
	onRefresh={async (options) => {
		requestOptions = options;
		await onUsersChanged();
		return users;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={UserMobileCardSnippet}
/>
