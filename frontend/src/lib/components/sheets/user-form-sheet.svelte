<script lang="ts">
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import UserPlusIcon from 'phosphor-svelte/lib/UserPlus';
	import SaveIcon from 'phosphor-svelte/lib/FloppyDisk';
	import type { User } from '$lib/types/user.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';

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
	let SubmitIcon = $derived(isEditMode ? SaveIcon : UserPlusIcon);

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

<Sheet.Root bind:open onOpenChange={handleOpenChange}>
	<Sheet.Content class="p-6">
		<Sheet.Header class="space-y-3 border-b pb-6">
			<div class="flex items-center gap-3">
				<div class="bg-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg">
					<SubmitIcon class="text-primary size-5" />
				</div>
				<div>
					<Sheet.Title class="text-xl font-semibold">
						{isEditMode ? m.users_edit_title() : m.users_create_new_title()}
					</Sheet.Title>
					<Sheet.Description class="text-muted-foreground mt-1 text-sm">
						{#if isEditMode}
							{m.users_edit_description({ username: userToEdit?.username ?? m.common_unknown() })}
						{:else}
							{m.users_create_description()}
						{/if}
					</Sheet.Description>
				</div>
			</div>
		</Sheet.Header>

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

			<Sheet.Footer class="flex flex-row gap-2">
				<Button
					type="button"
					class="arcane-button-cancel flex-1"
					variant="outline"
					onclick={() => (open = false)}
					disabled={isLoading}>{m.common_cancel()}</Button
				>
				<Button type="submit" class="arcane-button-create flex-1" disabled={isLoading}>
					{#if isLoading}
						<Spinner class="mr-2 size-4" />
					{/if}
					<SubmitIcon class="mr-2 size-4" />
					{isEditMode ? m.users_save_changes() : m.common_create_button({ resource: m.resource_user_cap() })}
				</Button>
			</Sheet.Footer>
		</form>
	</Sheet.Content>
</Sheet.Root>
