<script lang="ts">
	import { UsersIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import UserTable from './user-table.svelte';
	import UserFormSheet from '$lib/components/sheets/user-form-sheet.svelte';
	import type { SearchPaginationSortRequest } from '$lib/types/shared';
	import type { User } from '$lib/types/auth';
	import type { CreateUser } from '$lib/types/auth';
	import { m } from '$lib/paraglide/messages';
	import { userService } from '$lib/services/user-service';
	import { roleService } from '$lib/services/role-service';
	import userStore from '$lib/stores/user-store';
	import { untrack } from 'svelte';
	import { SettingsPageLayout, type SettingsActionButton } from '$lib/layouts/index.js';

	let { data } = $props();

	let users = $state(untrack(() => data.users));
	let selectedIds = $state<string[]>([]);
	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.userRequestOptions));

	let isDialogOpen = $state({
		create: false,
		edit: false
	});

	let userToEdit = $state<User | null>(null);

	// Role assignment is admin-only on the server. Non-admins editing users can
	// still update profile fields but must not call the assignments endpoint.
	const isAdmin = $derived(userStore.isGlobalAdmin());

	// availableRoleAssignments for the edit form — strip the source field, the
	// editor only needs (roleId, environmentId) tuples.
	const editingAssignments = $derived(
		userToEdit?.roleAssignments?.map((a) => ({ roleId: a.roleId, environmentId: a.environmentId })) ?? []
	);

	let isLoading = $state({
		creating: false,
		editing: false,
		refresh: false
	});

	function openCreateDialog() {
		userToEdit = null;
		isDialogOpen.create = true;
	}

	function openEditDialog(user: User) {
		userToEdit = user;
		isDialogOpen.edit = true;
	}

	async function handleUserSubmit({
		user,
		isEditMode,
		userId
	}: {
		user: Omit<Partial<User>, 'roleAssignments'> & {
			password?: string;
			roleAssignments?: { roleId: string; environmentId?: string }[];
		};
		isEditMode: boolean;
		userId?: string;
	}) {
		const loading = isEditMode ? 'editing' : 'creating';
		isLoading[loading] = true;

		try {
			if (isEditMode && userId) {
				const safeUsername = userToEdit?.username || m.common_unknown();
				// Split: profile fields go to PUT /users/{id}; role assignments
				// go to PUT /users/{id}/role-assignments (separate endpoint).
				const { roleAssignments, ...profile } = user;
				const result = await tryCatch(userService.update(userId, profile));
				handleApiResultWithCallbacks({
					result,
					message: m.common_update_failed({ resource: `${m.resource_user()} "${safeUsername}"` }),
					setLoadingState: (value) => (isLoading[loading] = value),
					onSuccess: async () => {
						if (isAdmin && roleAssignments) {
							await roleService.setUserAssignments(userId, { assignments: roleAssignments });
						}
						toast.success(m.common_update_success({ resource: `${m.resource_user()} "${safeUsername}"` }));
						users = await userService.getUsers(requestOptions);
						isDialogOpen.edit = false;
						userToEdit = null;
					}
				});
			} else {
				if (!user.username) {
					toast.error(m.common_username_required());
					isLoading[loading] = false;
					return;
				}

				const safeUsername = user.username!.trim() || m.common_unknown();

				const createUser: CreateUser = {
					username: user.username!,
					displayName: user.displayName,
					email: user.email,
					password: user.password!
				};

				const result = await tryCatch(userService.create(createUser));
				handleApiResultWithCallbacks({
					result,
					message: m.common_create_failed({ resource: `${m.resource_user()} "${safeUsername}"` }),
					setLoadingState: (value) => (isLoading[loading] = value),
					onSuccess: async (created) => {
						if (isAdmin && user.roleAssignments && created?.id) {
							await roleService.setUserAssignments(created.id, { assignments: user.roleAssignments });
						}
						toast.success(m.common_create_success({ resource: `${m.resource_user()} "${safeUsername}"` }));
						users = await userService.getUsers(requestOptions);
						isDialogOpen.create = false;
					}
				});
			}
		} catch (error) {
			console.error('Failed to submit user:', error);
		}
	}

	const actionButtons: SettingsActionButton[] = $derived.by(() => [
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_user_cap() }),
			onclick: openCreateDialog,
			loading: isLoading.creating,
			disabled: isLoading.creating
		}
	]);
</script>

<SettingsPageLayout
	title={m.users_title()}
	description={m.users_subtitle()}
	icon={UsersIcon}
	pageType="management"
	{actionButtons}
>
	{#snippet mainContent()}
		<UserTable
			bind:users
			bind:selectedIds
			bind:requestOptions
			roles={data.roles}
			onUsersChanged={async () => {
				users = await userService.getUsers(requestOptions);
			}}
			onEditUser={openEditDialog}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<UserFormSheet
			bind:open={isDialogOpen.create}
			userToEdit={null}
			roles={data.roles}
			environments={data.environments}
			availableRoleAssignments={[]}
			onSubmit={handleUserSubmit}
			isLoading={isLoading.creating}
		/>

		<UserFormSheet
			bind:open={isDialogOpen.edit}
			{userToEdit}
			roles={data.roles}
			environments={data.environments}
			availableRoleAssignments={editingAssignments}
			onSubmit={handleUserSubmit}
			isLoading={isLoading.editing}
			allowUsernameEdit={true}
		/>
	{/snippet}
</SettingsPageLayout>
