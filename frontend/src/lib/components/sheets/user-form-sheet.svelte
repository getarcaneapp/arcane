<script lang="ts">
	import { z } from 'zod/v4';

	import EffectiveAccessView from '#lib/components/effective-access/effective-access-view.svelte';
	import FormInput from '#lib/components/form/form-input.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import BindingsTable from '#lib/components/role-bindings/bindings-table.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import type { User, UserUpdate } from '#lib/types/auth.js';
	import type { Role } from '#lib/types/auth.js';
	import type { Environment } from '#lib/types/environment.js';
	import { isValidUserEmail } from '#lib/utils/formatting.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	type UserFormSubmission = { user: UserUpdate; isEditMode: boolean; userId?: string };

	type UserFormProps = {
		open: boolean;
		userToEdit: User | null;
		roles: Role[] | null;
		environments: Environment[];
		onSubmit: (data: UserFormSubmission) => Promise<boolean>;
		isLoading: boolean;
		allowUsernameEdit?: boolean;
	};

	let {
		open = $bindable(false),
		userToEdit = $bindable(),
		roles,
		environments,
		onSubmit,
		isLoading,
		allowUsernameEdit = false
	}: UserFormProps = $props();

	let isEditMode = $derived(!!userToEdit);
	let canEditUsername = $derived(!isEditMode || allowUsernameEdit);

	let isOidcUser = $derived(!!userToEdit?.oidcSubjectId);
	const userFieldMaxLength = 255;
	const passwordMinLength = 8;

	const formSchema = z.object({
		username: z
			.string()
			.trim()
			.refine((value) => value === userToEdit?.username.trim() || value.length > 0, m.common_username_required())
			.refine(
				(value) => value === userToEdit?.username.trim() || [...value].length <= userFieldMaxLength,
				m.common_max_length({ field: m.common_username(), maxLength: userFieldMaxLength })
			)
			// Legacy usernames may contain @; only new or changed usernames are restricted
			.refine((value) => value === userToEdit?.username.trim() || !value.includes('@'), m.users_username_no_at()),
		password: z
			.string()
			.refine((value) => (isEditMode && value === '') || [...value].length >= passwordMinLength, m.first_login_error_length()),
		displayName: z
			.string()
			.trim()
			.refine(
				(value) => [...value].length <= userFieldMaxLength,
				m.common_max_length({ field: m.common_display_name(), maxLength: userFieldMaxLength })
			),
		email: z
			.string()
			.trim()
			.refine((value) => value === '' || isValidUserEmail(value), m.common_invalid_email())
	});

	let formData = $derived({
		username: userToEdit?.username || '',
		password: '',
		displayName: userToEdit?.displayName || '',
		email: userToEdit?.email || ''
	});

	let form = $derived(createForm<typeof formSchema>(formSchema, formData));
	let inputs = $derived(form.inputs);

	async function submitUser(data: UserFormSubmission) {
		if (await onSubmit(data)) {
			form.reset();
		}
	}

	async function handleSubmit() {
		// createForm trims strings for ordinary text fields. Passwords are opaque,
		// so preserve exactly what the user entered after validating the raw value.
		const password = inputs.password.value;
		const data = form.validate();
		if (!data) return;

		if (isOidcUser) {
			handleOpenChange(false);
			return;
		}

		if (!userToEdit) {
			await submitUser({
				user: {
					username: data.username,
					password,
					displayName: data.displayName || undefined,
					email: data.email || undefined
				},
				isEditMode
			});
			return;
		}

		const profile = changedFields(
			{
				...userToEdit,
				username: userToEdit.username.trim(),
				displayName: userToEdit.displayName || null,
				email: userToEdit.email || null
			},
			{
				displayName: data.displayName || null,
				email: data.email || null,
				username: canEditUsername ? data.username : userToEdit.username.trim()
			}
		);

		await submitUser({
			user: {
				...profile,
				...(password ? { password } : {})
			},
			isEditMode,
			userId: userToEdit.id
		});
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			form.reset();
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
	contentClass="sm:max-w-240"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} novalidate class="grid gap-4 py-6">
			<FormInput
				label={m.common_username()}
				type="text"
				description={m.users_username_description()}
				disabled={!canEditUsername || isOidcUser}
				bind:input={inputs.username}
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
				bind:input={inputs.password}
			/>
			<FormInput
				label={m.common_display_name()}
				type="text"
				placeholder={m.users_display_name_placeholder()}
				description={m.users_display_name_description()}
				disabled={isOidcUser}
				bind:input={inputs.displayName}
			/>
			<FormInput
				label={m.common_email()}
				type="text"
				placeholder={m.user_example_com_placeholder()}
				description={m.users_email_description()}
				autocomplete="email"
				disabled={isOidcUser}
				bind:input={inputs.email}
			/>
			{#if !userToEdit}
				<p class="text-xs text-muted-foreground">{m.users_default_role_hint()}</p>
			{/if}
			<!-- fallow-ignore-next-line code-duplication -- per-sheet footer wrapper ({#snippet footer} -> shared SheetFooterActions); ResponsiveDialog requires a footer snippet in each sheet -->
		</form>
		{#if userToEdit}
			<IfPermitted perm={PERMISSIONS.BINDINGS_LIST}>
				<div class="grid gap-6 pb-6">
					{#if userToEdit.groups.length > 0}
						<div class="space-y-2 px-1">
							<h3 class="text-sm font-semibold">{m.groups_title()}</h3>
							<div class="flex flex-wrap gap-2">
								{#each userToEdit.groups as group (`${group.id}:${group.source}`)}
									<Badge variant={group.source === 'oidc' ? 'purple' : 'gray'}>
										{group.name}
										{#if group.source === 'oidc'}
											· {m.groups_member_source_oidc()}
										{/if}
									</Badge>
								{/each}
							</div>
							{#if isOidcUser}
								<p class="text-xs text-muted-foreground">{m.users_oidc_groups_hint()}</p>
							{/if}
						</div>
					{/if}
					<BindingsTable subjectType="user" subjectId={userToEdit.id} subjectName={userToEdit.username} {roles} {environments} />
					<EffectiveAccessView userId={userToEdit.id} {environments} />
				</div>
			</IfPermitted>
		{/if}
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			onCancel={() => handleOpenChange(false)}
			cancelDisabled={isLoading}
			submitAction={isEditMode ? 'save' : 'create'}
			submitDisabled={isLoading}
			submitLoading={isLoading}
			onSubmit={handleSubmit}
			submitLabel={isEditMode ? m.common_save_changes() : m.common_create_button({ resource: m.common_user() })}
		/>
	{/snippet}
</ResponsiveDialog.Root>
