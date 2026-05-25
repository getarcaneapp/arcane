<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import type { User } from '$lib/types/auth';
	import type { Role } from '$lib/types/auth';
	import { BUILT_IN_ROLE_ADMIN, BUILT_IN_ROLE_EDITOR, BUILT_IN_ROLE_DEPLOYER, BUILT_IN_ROLE_VIEWER } from '$lib/types/auth';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { userService } from '$lib/services/user-service';
	import { UserIcon, TrashIcon, EditIcon, EllipsisIcon } from '$lib/icons';
	import IfPermitted from '$lib/components/if-permitted.svelte';

	let {
		users = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		roles,
		onUsersChanged,
		onEditUser
	}: {
		users: Paginated<User>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		roles: Role[];
		onUsersChanged: () => Promise<void>;
		onEditUser: (user: User) => void;
	} = $props();

	const rolesById = $derived(new Map(roles.map((r) => [r.id, r])));

	let isLoading = $state({
		removing: false
	});

	function canDeleteUser(user: User) {
		return user.canDelete !== false;
	}

	const selectedUsers = $derived.by(() =>
		(selectedIds ?? []).map((id) => users.data.find((user) => user.id === id)).filter((user): user is User => Boolean(user))
	);

	const hasProtectedSelection = $derived.by(() => selectedUsers.some((user) => !canDeleteUser(user)));

	async function handleDeleteSelected() {
		if (selectedIds.length === 0 || hasProtectedSelection) return;

		openConfirmDialog({
			title: m.users_delete_selected_title({ count: selectedIds.length }),
			message: m.users_delete_selected_message({ count: selectedIds.length, users: selectedIds.length }),
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

	async function handleDeleteUser(user: User) {
		if (!canDeleteUser(user)) return;

		const safeName = user.username?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.users_delete_user_title({ username: safeName }),
			message: m.users_delete_user_message({ username: safeName }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(userService.delete(user.id)),
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

	type BadgeVariant = 'red' | 'blue' | 'purple' | 'gray' | 'green';

	/** True when the user holds the built-in Admin role globally. */
	function isGlobalAdmin(user: User): boolean {
		return !!user.roleAssignments?.some((a) => a.roleId === BUILT_IN_ROLE_ADMIN && !a.environmentId);
	}

	/** Color variant for a role badge keyed off the role ID (not its localized name). */
	function roleVariant(roleId: string): BadgeVariant {
		switch (roleId) {
			case BUILT_IN_ROLE_ADMIN:
				return 'red';
			case BUILT_IN_ROLE_EDITOR:
				return 'blue';
			case BUILT_IN_ROLE_DEPLOYER:
				return 'purple';
			case BUILT_IN_ROLE_VIEWER:
				return 'gray';
			default:
				return 'green'; // custom roles
		}
	}

	function roleName(roleId: string): string {
		return rolesById.get(roleId)?.name ?? m.common_unknown();
	}

	/**
	 * Resolves what to render in the Role column for a given user. Three states:
	 * - no assignments       → single "No access" gray badge
	 * - holds global Admin   → single "Admin" red badge (collapses any other rows)
	 * - everything else      → one badge per distinct role, with a tooltip
	 *                          listing which environments each scope applies to
	 */
	function rolesSummary(user: User): { text: string; variant: BadgeVariant; tooltip?: string }[] {
		const assignments = user.roleAssignments ?? [];
		if (assignments.length === 0) {
			return [{ text: m.users_role_summary_none(), variant: 'gray' }];
		}
		if (isGlobalAdmin(user)) {
			return [{ text: m.common_admin(), variant: 'red' }];
		}
		// Group by roleId so a user assigned the same role on multiple envs gets
		// one badge with a tooltip listing the env count.
		const byRole = new Map<string, number>();
		for (const a of assignments) {
			byRole.set(a.roleId, (byRole.get(a.roleId) ?? 0) + 1);
		}
		return Array.from(byRole.entries()).map(([roleId, count]) => ({
			text: roleName(roleId),
			variant: roleVariant(roleId),
			tooltip: count > 1 ? m.users_role_summary_env_count({ count }) : undefined
		}));
	}

	const columns = [
		{ accessorKey: 'username', title: m.common_username(), sortable: true, cell: UsernameCell },
		{ accessorKey: 'displayName', title: m.common_display_name(), sortable: true, cell: DisplayNameCell },
		{ accessorKey: 'email', title: m.common_email(), sortable: true, cell: EmailCell },
		{ accessorKey: 'roleAssignments', title: m.common_role(), sortable: false, cell: RoleCell }
	] satisfies ColumnSpec<User>[];

	const mobileFields = [
		{ id: 'displayName', label: m.common_display_name(), defaultVisible: true },
		{ id: 'email', label: m.common_email(), defaultVisible: true },
		{ id: 'roleAssignments', label: m.common_role(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleDeleteSelected,
			loading: isLoading.removing,
			disabled: isLoading.removing || hasProtectedSelection,
			icon: TrashIcon
		}
	]);

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
	<div class="flex flex-wrap items-center gap-1">
		{#each rolesSummary(item) as badge (badge.text)}
			<StatusBadge text={badge.text} variant={badge.variant} tooltip={badge.tooltip} />
		{/each}
	</div>
{/snippet}

{#snippet UserMobileCardSnippet({ item, mobileFieldVisibility }: { item: User; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: UserIcon, variant: 'blue' }}
		title={(item: User) => item.username}
		subtitle={(item: User) => ((mobileFieldVisibility['email'] ?? true) && item.email ? item.email : null)}
		badges={[
			(item: User) => {
				if (!(mobileFieldVisibility['roleAssignments'] ?? true)) return null;
				const summary = rolesSummary(item);
				// Mobile cards take a single badge; show the first (or a "+N" hint
				// when the user has multiple distinct roles).
				const [head, ...rest] = summary;
				if (!head) return null;
				if (rest.length === 0) {
					return { variant: head.variant, text: head.text };
				}
				return { variant: head.variant, text: `${head.text} +${rest.length}` };
			}
		]}
		fields={[
			{
				label: m.common_display_name(),
				getValue: (item: User) => item.displayName,
				icon: UserIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility['displayName'] ?? true) && !!item.displayName
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: User })}
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
				{#if !item.oidcSubjectId}
					<IfPermitted perm="users:update">
						<DropdownMenu.Item onclick={() => onEditUser(item)}>
							<EditIcon class="size-4" />
							{m.common_edit()}
						</DropdownMenu.Item>

						<DropdownMenu.Separator />
					</IfPermitted>
				{/if}

				<IfPermitted perm="users:delete">
					<DropdownMenu.Item
						variant="destructive"
						disabled={!canDeleteUser(item) || isLoading.removing}
						onclick={() => handleDeleteUser(item)}
					>
						<TrashIcon class="size-4" />
						{m.common_delete()}
					</DropdownMenu.Item>
				</IfPermitted>
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
	{bulkActions}
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
