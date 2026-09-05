<script lang="ts">
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import FormInput from '#lib/components/form/form-input.svelte';
	import RoleAssignmentsEditor from '#lib/components/forms/role-assignments-editor.svelte';
	import type { User } from '#lib/types/auth';
	import type { Role } from '#lib/types/auth';
	import type { Environment } from '#lib/types/environment';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '#lib/utils/settings';
	import { isValidUserEmail } from '#lib/utils/formatting';
	import { m } from '#lib/paraglide/messages';
	import IfPermitted from '#lib/components/if-permitted.svelte';

	type RoleAssignmentInput = { roleId: string; environmentId?: string };
	type UserSubmission = Omit<Partial<User>, 'roleAssignments'> & {
		password?: string;
		roleAssignments?: RoleAssignmentInput[];
	};
	type UserFormSubmission = { user: UserSubmission; isEditMode: boolean; userId?: string };

	type UserFormProps = {
		open: boolean;
		userToEdit: User | null;
		roles: Role[];
		environments: Environment[];
		availableRoleAssignments?: RoleAssignmentInput[];
		onSubmit: (data: UserFormSubmission) => Promise<boolean>;
		isLoading: boolean;
		allowUsernameEdit?: boolean;
	};

	let {
		open = $bindable(false),
		userToEdit = $bindable(),
		roles,
		environments,
		availableRoleAssignments = [],
		onSubmit,
		isLoading,
		allowUsernameEdit = false
	}: UserFormProps = $props();

	let isEditMode = $derived(!!userToEdit);
	let canEditUsername = $derived(!isEditMode || allowUsernameEdit);

	let isOidcUser = $derived(!!userToEdit?.oidcSubjectId);
	let oidcAssignments = $derived(userToEdit?.roleAssignments?.filter((a) => a.source === 'oidc') ?? []);
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
			.refine((value) => value === '' || isValidUserEmail(value), m.common_invalid_email()),
		roleAssignments: z
			.array(
				z.object({
					roleId: z.string().min(1),
					environmentId: z.string().optional()
				})
			)
			// OIDC users may get all their roles from OIDC mappings, so zero
			// manual assignments is valid for them.
			.refine((assignments) => isOidcUser || assignments.length >= 1, m.users_role_assignments_required())
	});

	let formData = $derived({
		username: userToEdit?.username || '',
		password: '',
		displayName: userToEdit?.displayName || '',
		email: userToEdit?.email || '',
		roleAssignments: availableRoleAssignments
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	async function submitUser(data: UserFormSubmission) {
		if (await onSubmit(data)) {
			form.reset();
		}
	}

	async function handleSubmit() {
		// createForm trims strings for ordinary text fields. Passwords are opaque,
		// so preserve exactly what the user entered after validating the raw value.
		const password = $inputs.password.value;
		const data = form.validate();
		if (!data) return;

		// For OIDC users, only allow role assignment changes
		if (isOidcUser) {
			await submitUser({
				user: { roleAssignments: data.roleAssignments },
				isEditMode,
				userId: userToEdit?.id
			});
			return;
		}

		const userData: UserSubmission = {
			displayName: data.displayName,
			roleAssignments: data.roleAssignments
		};

		if (data.email) {
			userData.email = data.email;
		}

		// Only include username when creating, or when editing changed it — an
		// unchanged legacy username with @ would fail the backend pattern check
		if (!isEditMode || (canEditUsername && data.username !== userToEdit?.username.trim())) {
			userData.username = data.username;
		}

		// Only include password if it's provided (for create) or if editing and password is not empty
		if (!isEditMode || (isEditMode && password)) {
			userData.password = password;
		}

		await submitUser({ user: userData, isEditMode, userId: userToEdit?.id });
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
	contentClass="sm:max-w-[640px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} novalidate class="grid gap-4 py-6">
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
				type="text"
				placeholder={m.user_example_com_placeholder()}
				description={m.users_email_description()}
				autocomplete="email"
				disabled={isOidcUser}
				bind:input={$inputs.email}
			/>
			<IfPermitted adminOnly>
				<div>
					<label for="roleAssignments" class="text-sm font-medium">{m.users_role_assignments_label()}</label>
					<p class="mb-2 text-xs text-muted-foreground">{m.users_role_assignments_description()}</p>
					{#if oidcAssignments.length > 0}
						<div class="mb-3">
							<p class="text-xs text-muted-foreground">
								{m.oidc_synced_role_assignments()}
								<a href="/settings/authentication" class="ml-1 text-primary underline">{m.users_role_assignments_oidc_link()}</a>
							</p>
							<div class="mt-2 flex flex-wrap gap-2">
								{#each oidcAssignments as assignment (`${assignment.roleId}-${assignment.environmentId ?? 'global'}`)}
									<Badge variant="outline">
										{roles.find((r) => r.id === assignment.roleId)?.name ?? assignment.roleId}
										·
										{environments.find((e) => e.id === assignment.environmentId)?.name ?? m.global_org_wide()}
									</Badge>
								{/each}
							</div>
						</div>
					{/if}
					<RoleAssignmentsEditor bind:assignments={$inputs.roleAssignments.value} {roles} {environments} />
					{#if $inputs.roleAssignments.error}
						<p class="mt-1 text-xs text-destructive">{$inputs.roleAssignments.error}</p>
					{/if}
				</div>
			</IfPermitted>
			<!-- fallow-ignore-next-line code-duplication -- per-sheet footer wrapper ({#snippet footer} -> shared SheetFooterActions); ResponsiveDialog requires a footer snippet in each sheet -->
		</form>
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
