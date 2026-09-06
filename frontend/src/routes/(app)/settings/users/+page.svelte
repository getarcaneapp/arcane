<script lang="ts">
	import { UsersIcon } from '#lib/icons/index.js';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/api.js';
	import UserTable from './user-table.svelte';
	import UserFormSheet from '#lib/components/sheets/user-form-sheet.svelte';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { Settings } from '#lib/types/settings.js';
	import type { User } from '#lib/types/auth.js';
	import type { CreateUser } from '#lib/types/auth.js';
	import { m } from '#lib/paraglide/messages.js';
	import { userService } from '#lib/services/user-service.js';
	import { roleService } from '#lib/services/role-service.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import userStore from '#lib/stores/user-store.js';
	import settingsStore from '#lib/stores/config-store.js';
	import { SettingsPageLayout, type SettingsActionButton } from '#lib/layouts/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Input } from '#lib/components/ui/input/index.js';

	let { data } = $props();

	let users = $derived(data.users);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived<SearchPaginationSortRequest>(data.userRequestOptions);

	let isDialogOpen = $state({
		create: false,
		edit: false
	});

	let userToEdit = $state<User | null>(null);

	// Role assignment is admin-only on the server. Non-admins editing users can
	// still update profile fields but must not call the assignments endpoint.
	const isAdmin = $derived(userStore.isGlobalAdmin());

	// availableRoleAssignments for the edit form — manual assignments only,
	// stripped to (roleId, environmentId) tuples. OIDC-sourced assignments must
	// not round-trip: PUT /users/{id}/role-assignments replaces source='manual'
	// rows, so resubmitting oidc rows would duplicate them as manual.
	const editingAssignments = $derived(
		userToEdit?.roleAssignments
			?.filter((a) => a.source !== 'oidc')
			.map((a) => ({ roleId: a.roleId, environmentId: a.environmentId })) ?? []
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

	async function completeUserEdit(username: string) {
		toast.success(m.common_update_success({ resource: `${m.resource_user()} "${username}"` }));
		users = await userService.getUsers(requestOptions);
		isDialogOpen.edit = false;
		userToEdit = null;
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
	}): Promise<boolean> {
		const loading = isEditMode ? 'editing' : 'creating';
		isLoading[loading] = true;

		try {
			if (isEditMode && userId) {
				const safeUsername = userToEdit?.username || m.common_unknown();
				// Split: profile fields go to PUT /users/{id}; role assignments
				// go to PUT /users/{id}/role-assignments (separate endpoint).
				const { roleAssignments, ...profile } = user;

				// OIDC users submit role assignments only — skip the empty profile PUT.
				if (Object.keys(profile).length === 0 && isAdmin && roleAssignments) {
					const assignmentsResult = await tryCatch(roleService.setUserAssignments(userId, { assignments: roleAssignments }));
					await handleApiResultWithCallbacks({
						result: assignmentsResult,
						message: m.common_update_failed({ resource: `${m.resource_user()} "${safeUsername}"` }),
						setLoadingState: (value) => (isLoading[loading] = value),
						onSuccess: async () => {
							await completeUserEdit(safeUsername);
						}
					});
					return !assignmentsResult.error;
				}

				const result = await tryCatch(userService.update(userId, profile));
				await handleApiResultWithCallbacks({
					result,
					message: m.common_update_failed({ resource: `${m.resource_user()} "${safeUsername}"` }),
					setLoadingState: (value) => (isLoading[loading] = value),
					onSuccess: async () => {
						if (isAdmin && roleAssignments) {
							await roleService.setUserAssignments(userId, { assignments: roleAssignments });
						}
						await completeUserEdit(safeUsername);
					}
				});
				return !result.error;
			} else {
				if (!user.username) {
					toast.error(m.common_username_required());
					isLoading[loading] = false;
					return false;
				}

				const safeUsername = user.username!.trim() || m.common_unknown();

				const createUser: CreateUser = {
					username: user.username!,
					displayName: user.displayName,
					email: user.email,
					password: user.password!
				};

				const result = await tryCatch(userService.create(createUser));
				await handleApiResultWithCallbacks({
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
				return !result.error;
			}
		} catch (error) {
			console.error('Failed to submit user:', error);
			return false;
		}
	}

	// Avatar policy: server-wide settings that belong with user management.
	const isReadOnly = $derived(Boolean($settingsStore?.uiConfigDisabled));
	const gravatarEnabled = $derived(Boolean($settingsStore?.enableGravatar));
	const avatarMaxUploadSizeMb = $derived(
		Number($settingsStore?.avatarMaxUploadSizeMb) > 0 ? Number($settingsStore?.avatarMaxUploadSizeMb) : 2
	);
	let avatarSizeInput = $state('');
	let avatarSizeError = $state<string | null>(null);

	$effect(() => {
		avatarSizeInput = String(avatarMaxUploadSizeMb);
	});

	async function saveAvatarSettings(patch: Partial<Settings>) {
		try {
			const updated = await settingsService.updateSettings(patch);
			settingsStore.set(updated);
			toast.success(m.common_update_success({ resource: m.settings() }));
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_update_failed({ resource: m.settings() }));
		}
	}

	function handleAvatarSizeCommit() {
		const parsed = Number(avatarSizeInput);
		if (!Number.isInteger(parsed) || parsed < 1 || parsed > 50) {
			avatarSizeError = m.general_avatar_upload_size_help();
			return;
		}
		avatarSizeError = null;
		if (parsed === avatarMaxUploadSizeMb) return;
		void saveAvatarSettings({ avatarMaxUploadSizeMb: parsed });
	}

	const actionButtons: SettingsActionButton[] = $derived.by(() => [
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.common_user() }),
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
		<div class="mb-6 divide-y divide-border/40 border-b border-border/50 pb-6 [&>*]:py-5 [&>*:first-child]:pt-0">
			<SettingsRow
				label={m.general_enable_gravatar_label()}
				description={m.general_enable_gravatar_description()}
				layout="inline"
			>
				<Switch
					id="enableGravatar"
					checked={gravatarEnabled}
					disabled={isReadOnly}
					onCheckedChange={(checked) => void saveAvatarSettings({ enableGravatar: checked })}
				/>
			</SettingsRow>

			<SettingsRow
				label={m.general_avatar_upload_size_label()}
				description={m.general_avatar_upload_size_description()}
				helpText={m.general_avatar_upload_size_help()}
				layout="inline"
			>
				<div class="flex w-24 flex-col gap-1">
					<Input
						id="avatarMaxUploadSizeMb"
						type="number"
						min="1"
						max="50"
						bind:value={avatarSizeInput}
						placeholder="2"
						disabled={isReadOnly}
						aria-invalid={Boolean(avatarSizeError)}
						onblur={handleAvatarSizeCommit}
					/>
					{#if avatarSizeError}
						<p class="text-xs font-medium text-destructive">{avatarSizeError}</p>
					{/if}
				</div>
			</SettingsRow>
		</div>

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
			bind:userToEdit
			roles={data.roles}
			environments={data.environments}
			availableRoleAssignments={editingAssignments}
			onSubmit={handleUserSubmit}
			isLoading={isLoading.editing}
			allowUsernameEdit={true}
		/>
	{/snippet}
</SettingsPageLayout>
