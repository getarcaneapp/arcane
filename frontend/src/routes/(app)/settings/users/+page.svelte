<script lang="ts">
	import UsersIcon from '@lucide/svelte/icons/users';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import UserTable from './user-table.svelte';
	import UserFormSheet from '$lib/components/sheets/user-form-sheet.svelte';
	import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { User } from '$lib/types/user.type';
	import type { CreateUser } from '$lib/types/user.type';
	import { m } from '$lib/paraglide/messages';
	import { userService } from '$lib/services/user-service';
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
		user: Partial<User> & { password?: string };
		isEditMode: boolean;
		userId?: string;
	}) {
		const loading = isEditMode ? 'editing' : 'creating';
		isLoading[loading] = true;

		try {
			if (isEditMode && userId) {
				const safeUsername = user.username?.trim() || m.common_unknown();
				const result = await tryCatch(userService.update(userId, user));
				handleApiResultWithCallbacks({
					result,
					message: m.common_update_failed({ resource: `${m.resource_user()} "${safeUsername}"` }),
					setLoadingState: (value) => (isLoading[loading] = value),
					onSuccess: async () => {
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
					password: user.password!,
					roles: user.roles ?? ['user']
				};

				const result = await tryCatch(userService.create(createUser));
				handleApiResultWithCallbacks({
					result,
					message: m.common_create_failed({ resource: `${m.resource_user()} "${safeUsername}"` }),
					setLoadingState: (value) => (isLoading[loading] = value),
					onSuccess: async () => {
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
	statCardsColumns={3}
>
	{#snippet mainContent()}
		<UserTable
			bind:users
			bind:selectedIds
			bind:requestOptions
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
			roles={[]}
			onSubmit={handleUserSubmit}
			isLoading={isLoading.creating}
		/>

		<UserFormSheet
			bind:open={isDialogOpen.edit}
			{userToEdit}
			roles={[]}
			onSubmit={handleUserSubmit}
			isLoading={isLoading.editing}
		/>
	{/snippet}
</SettingsPageLayout>
