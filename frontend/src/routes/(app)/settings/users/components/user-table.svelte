<script lang="ts">
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { UserIcon, TrashIcon, EditIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { userService } from '#lib/services/user-service.js';
	import type { User } from '#lib/types/auth.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { bulkConfirmAndRun, confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { roleBadgeVariant, type RoleBadgeVariant } from '#lib/utils/rbac.js';

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

	function canDeleteUser(user: User) {
		return user.canDelete !== false;
	}

	const selectedUsers = $derived.by(() =>
		(selectedIds ?? []).map((id) => users.data.find((user) => user.id === id)).filter((user): user is User => Boolean(user))
	);

	const hasProtectedSelection = $derived.by(() => selectedUsers.some((user) => !canDeleteUser(user)));

	function handleDeleteSelected() {
		if (selectedIds.length === 0 || hasProtectedSelection) return;
		const ids = [...selectedIds];

		bulkConfirmAndRun({
			ids,
			title: m.users_delete_selected_title({ count: ids.length }),
			message: m.users_delete_selected_message({ count: ids.length, users: ids.length }),
			confirmLabel: m.common_delete(),
			destructive: true,
			run: (userId) => userService.delete(userId),
			messages: {
				success: (count) => m.common_bulk_delete_success({ count, resource: m.users_title() }),
				partial: (success, total, failed) => m.common_bulk_delete_partial({ success, total, failed, resource: m.users_title() }),
				failure: () => m.common_bulk_delete_failed({ count: ids.length, resource: m.users_title() })
			},
			setLoading: (loading) => (isLoading.removing = loading),
			onComplete: async ({ success }) => {
				if (success > 0) await onUsersChanged();
			},
			clearSelection: () => (selectedIds = []),
			sequential: true
		});
	}

	function handleDeleteUser(user: User) {
		if (!canDeleteUser(user)) return;

		const safeName = user.username?.trim() || m.common_unknown();
		confirmAndRun({
			title: m.users_delete_user_title({ username: safeName }),
			message: m.users_delete_user_message({ username: safeName }),
			confirmLabel: m.common_delete(),
			destructive: true,
			setLoading: (loading) => (isLoading.removing = loading),
			run: () => userService.delete(user.id),
			failureMessage: m.users_delete_user_failed({ username: safeName }),
			onSuccess: async () => {
				toast.success(m.users_delete_user_success({ username: safeName }));
				await onUsersChanged();
			}
		});
	}

	function isGlobalAdmin(user: User): boolean {
		return user.isGlobalAdmin === true;
	}

	/**
	 * Resolves what to render in the Role column for a given user. Three states:
	 * - no assignments       → single "No access" gray badge
	 * - holds global Admin   → single "Admin" red badge (collapses any other rows)
	 * - everything else      → one badge per distinct role, with a tooltip
	 *                          listing which environments each scope applies to
	 */
	function rolesSummary(user: User): { text: string; variant: RoleBadgeVariant; tooltip?: string }[] {
		const bindings = user.bindings ?? [];
		if (bindings.length === 0) {
			return [{ text: m.users_role_summary_none(), variant: 'gray' }];
		}
		if (isGlobalAdmin(user)) {
			return [{ text: m.common_admin(), variant: 'red' }];
		}
		// Group by roleId so a user assigned the same role on multiple envs gets
		// one badge with a tooltip listing the env count.
		// A Set per role, since manual and OIDC can both grant the same environment.
		const byRole = new Map<string, { name: string; envs: Set<string> }>();
		for (const binding of bindings) {
			const entry = byRole.get(binding.roleId) ?? { name: binding.roleName, envs: new Set<string>() };
			entry.envs.add(binding.environmentId ?? 'global');
			byRole.set(binding.roleId, entry);
		}
		return Array.from(byRole.entries()).map(([roleId, entry]) => ({
			text: entry.name,
			variant: roleBadgeVariant(roleId),
			tooltip: entry.envs.size > 1 ? m.users_role_summary_env_count({ count: entry.envs.size }) : undefined
		}));
	}

	const columns = [
		{ accessorKey: 'username', title: m.common_username(), sortable: true, cell: UsernameCell },
		{ accessorKey: 'displayName', title: m.common_display_name(), sortable: true, cell: DisplayNameCell },
		{ accessorKey: 'email', title: m.common_email(), sortable: true, cell: EmailCell },
		{ accessorKey: 'bindings', title: m.common_role(), sortable: false, cell: RoleCell }
	] satisfies ColumnSpec<User>[];

	const mobileFields = [
		{ id: 'displayName', label: m.common_display_name(), defaultVisible: true },
		{ id: 'email', label: m.common_email(), defaultVisible: true },
		{ id: 'bindings', label: m.common_role(), defaultVisible: true }
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
			{#if badge.tooltip}
				<ArcaneTooltip.Root>
					<ArcaneTooltip.Trigger>
						<Badge variant={badge.variant} minWidth="20">{badge.text}</Badge>
					</ArcaneTooltip.Trigger>
					<ArcaneTooltip.Content>
						<p class="max-w-xs text-xs">{badge.tooltip}</p>
					</ArcaneTooltip.Content>
				</ArcaneTooltip.Root>
			{:else}
				<Badge variant={badge.variant} minWidth="20">{badge.text}</Badge>
			{/if}
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
				if (!(mobileFieldVisibility['bindings'] ?? true)) return null;
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
	<RowActionsMenu>
		<IfPermitted perm={PERMISSIONS.USERS_UPDATE}>
			<DropdownMenu.Item onclick={() => onEditUser(item)}>
				<EditIcon class="size-4" />
				{m.common_edit()}
			</DropdownMenu.Item>

			<DropdownMenu.Separator />
		</IfPermitted>

		<IfPermitted perm={PERMISSIONS.USERS_DELETE}>
			<DropdownMenu.Item
				variant="destructive"
				disabled={!canDeleteUser(item) || isLoading.removing}
				onclick={() => handleDeleteUser(item)}
			>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
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
