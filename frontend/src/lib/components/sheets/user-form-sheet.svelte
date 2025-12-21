<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import type { User } from '$lib/types/user.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { AddIcon, SaveIcon } from '$lib/icons';

	type UserFormProps = {
		open: boolean;
		userToEdit: User | null;
		roles: { id: string; name: string }[];
		onSubmit: (data: { user: Partial<User> & { password?: string }; isEditMode: boolean; userId?: string }) => void;
		isLoading: boolean;
		allowUsernameEdit?: boolean;
	};

	let {
		open = $bindable(false),
		userToEdit = $bindable(),
		roles,
		onSubmit,
		isLoading,
		allowUsernameEdit = false
	}: UserFormProps = $props();

	let isEditMode = $derived(!!userToEdit);
	let canEditUsername = $derived(!isEditMode || allowUsernameEdit);
	let SubmitIcon = $derived(isEditMode ? SaveIcon : AddIcon);

	let isOidcUser = $derived(!!userToEdit?.oidcSubjectId);

	const formSchema = z.object({
		username: z.string().min(1, m.common_username_required()),
		password: z.string().optional(),
		displayName: z.string().optional(),
		email: z.email(m.common_invalid_email()).optional().or(z.literal('')),
		isAdmin: z.boolean().default(false)
	});

	let formData = $derived({
		username: userToEdit?.username || '',
		password: '',
		displayName: userToEdit?.displayName || '',
		email: userToEdit?.email || '',
		isAdmin: Boolean(userToEdit?.roles?.includes('admin'))
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		// For OIDC users, only allow role changes
		if (isOidcUser) {
			onSubmit({
				user: { roles: [data.isAdmin ? 'admin' : 'user'] },
				isEditMode,
				userId: userToEdit?.id
			});
			return;
		}

		const userData: Partial<User> & { password?: string } = {
			username: data.username,
			displayName: data.displayName,
			email: data.email,
			roles: [data.isAdmin ? 'admin' : 'user']
		};

		// Only include password if it's provided (for create) or if editing and password is not empty
		if (!isEditMode || (isEditMode && data.password)) {
			userData.password = data.password;
		}

		onSubmit({ user: userData, isEditMode, userId: userToEdit?.id });
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			userToEdit = null;
		}
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.users_edit_title() : m.users_create_new_title()}
	description={isEditMode
		? m.users_edit_description({ username: userToEdit?.username ?? m.common_unknown() })
		: m.users_create_description()}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput
				label={m.common_username()}
				type="text"
				description={m.users_username_description()}
				disabled={!canEditUsername || isOidcUser}
				bind:input={$inputs.username}
			/>
			<FormInput
				label={isEditMode ? m.common_password() : m.users_password_required_label()}
				type="password"
				placeholder={isOidcUser
					? m.users_password_managed_oidc()
					: isEditMode
						? m.users_password_leave_empty()
						: m.users_password_enter()}
				description={isOidcUser
					? m.users_password_description_oidc()
					: isEditMode
						? m.users_password_description_edit()
						: m.users_password_description_create()}
				disabled={isOidcUser}
				bind:input={$inputs.password}
			/>
			<FormInput
				label={m.common_display_name()}
				type="text"
				placeholder={m.users_display_name_placeholder()}
				description={m.users_display_name_description()}
				disabled={isOidcUser}
				bind:input={$inputs.displayName}
			/>
			<FormInput
				label={m.common_email()}
				type="email"
				placeholder={m.users_email_placeholder()}
				description={m.users_email_description()}
				disabled={isOidcUser}
				bind:input={$inputs.email}
			/>
			<SwitchWithLabel
				id="isAdminSwitch"
				label={m.common_admin()}
				description={m.users_administrator_description()}
				bind:checked={$inputs.isAdmin.value}
			/>
		</form>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-row gap-2">
			<ArcaneButton
				action="cancel"
				tone="outline"
				type="button"
				class="flex-1"
				onclick={() => (open = false)}
				disabled={isLoading}
			/>
			<ArcaneButton
				action={isEditMode ? 'save' : 'create'}
				type="submit"
				class="flex-1"
				disabled={isLoading}
				loading={isLoading}
				onclick={handleSubmit}
				customLabel={isEditMode ? m.users_save_changes() : m.common_create_button({ resource: m.resource_user_cap() })}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
