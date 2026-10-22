<script lang="ts">
	import { toast } from 'svelte-sonner';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import UserFormSheet from '#lib/components/sheets/user-form-sheet.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { UsersIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { userService } from '#lib/services/user-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { User, UserCreate, UserUpdate } from '#lib/types/auth.js';
	import type { Settings } from '#lib/types/settings.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import UserTable from './components/user-table.svelte';

	let { data } = $props();

	let users = $derived(data.users);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived<SearchPaginationSortRequest>(data.userRequestOptions);

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

	async function refreshUsers() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(userService.getUsers(requestOptions)),
			message: m.common_refresh_failed({ resource: m.users_title() }),
			onSuccess: (list) => {
				users = list;
			}
		});
	}

	async function completeUserEdit(username: string) {
		toast.success(m.common_update_success({ resource: `${m.resource_user()} "${username}"` }));
		isDialogOpen.edit = false;
		userToEdit = null;
		await refreshUsers();
	}

	async function handleUserSubmit({
		user,
		isEditMode,
		userId
	}: {
		user: UserUpdate;
		isEditMode: boolean;
		userId?: string;
	}): Promise<boolean> {
		const loading = isEditMode ? 'editing' : 'creating';
		isLoading[loading] = true;

		if (isEditMode && userId) {
			const safeUsername = userToEdit?.username || m.common_unknown();
			const result = await tryCatch(userService.update(userId, user));
			await handleApiResultWithCallbacks({
				result,
				message: m.common_update_failed({ resource: `${m.resource_user()} "${safeUsername}"` }),
				setLoadingState: (value) => (isLoading[loading] = value),
				onSuccess: async () => {
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

			const createUser: UserCreate = {
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
				onSuccess: async () => {
					toast.success(m.common_create_success({ resource: `${m.resource_user()} "${safeUsername}"` }));
					isDialogOpen.create = false;
					await refreshUsers();
				}
			});
			return !result.error;
		}
	}

	// Avatar policy: server-wide settings that belong with user management.
	const gravatarEnabled = $derived(Boolean(settingsStore.current?.enableGravatar));
	const avatarMaxUploadSizeMb = $derived(
		Number(settingsStore.current?.avatarMaxUploadSizeMb) > 0 ? Number(settingsStore.current?.avatarMaxUploadSizeMb) : 2
	);
	let avatarSizeInput = $derived(String(avatarMaxUploadSizeMb));
	let avatarSizeError = $state<string | null>(null);

	async function saveAvatarSettings(patch: Partial<Settings>) {
		await handleApiResultWithCallbacks({
			result: await tryCatch(settingsStore.save(patch)),
			message: m.common_update_failed({ resource: m.settings() }),
			onSuccess: () => {
				toast.success(m.common_update_success({ resource: m.settings() }));
			}
		});
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

	const actionButtons: ActionButton[] = $derived.by(() => [
		{
			id: 'create',
			action: 'create',
			placement: 'primary',
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
		<SettingsSection title={m.settings_section_avatars()} description={m.settings_section_avatars_description()} class="mb-8">
			<SettingsRow
				for="enableGravatar"
				label={m.general_enable_gravatar_label()}
				description={m.general_enable_gravatar_description()}
				layout="switch"
				lock={settingsStore.lock('enableGravatar')}
			>
				<Switch
					id="enableGravatar"
					checked={gravatarEnabled}
					onCheckedChange={(checked) => void saveAvatarSettings({ enableGravatar: checked })}
				/>
			</SettingsRow>
			<SettingsRow
				for="avatarMaxUploadSizeMb"
				label={m.general_avatar_upload_size_label()}
				description={m.general_avatar_upload_size_description()}
				helpText={m.general_avatar_upload_size_help()}
				error={avatarSizeError}
				lock={settingsStore.lock('avatarMaxUploadSizeMb')}
			>
				<Input
					id="avatarMaxUploadSizeMb"
					type="number"
					min="1"
					max="50"
					bind:value={avatarSizeInput}
					placeholder="2"
					aria-invalid={Boolean(avatarSizeError)}
					onblur={handleAvatarSizeCommit}
				/>
			</SettingsRow>
		</SettingsSection>

		<UserTable bind:users bind:selectedIds bind:requestOptions onUsersChanged={refreshUsers} onEditUser={openEditDialog} />
	{/snippet}

	{#snippet additionalContent()}
		<UserFormSheet
			bind:open={isDialogOpen.create}
			userToEdit={null}
			roles={data.roles}
			environments={data.environments}
			onSubmit={handleUserSubmit}
			isLoading={isLoading.creating}
		/>

		<UserFormSheet
			bind:open={isDialogOpen.edit}
			bind:userToEdit
			roles={data.roles}
			environments={data.environments}
			onSubmit={handleUserSubmit}
			isLoading={isLoading.editing}
			allowUsernameEdit={true}
		/>
	{/snippet}
</SettingsPageLayout>
