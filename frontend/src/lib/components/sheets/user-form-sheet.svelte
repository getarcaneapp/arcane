<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import RoleAssignmentsEditor from '$lib/components/forms/role-assignments-editor.svelte';
	import type { User } from '$lib/types/auth';
	import type { Role } from '$lib/types/auth';
	import type { Environment } from '$lib/types/environment';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import { isValidUserEmail } from '$lib/utils/formatting';
	import { m } from '$lib/paraglide/messages';
	import IfPermitted from '$lib/components/if-permitted.svelte';

	type RoleAssignmentInput = { roleId: string; environmentId?: string };
	type UserSubmission = Omit<Partial<User>, 'roleAssignments'> & {
		password?: string;
		roleAssignments?: RoleAssignmentInput[];
	};

	type UserFormProps = {
		open: boolean;
		userToEdit: User | null;
		roles: Role[];
		environments: Environment[];
		availableRoleAssignments?: RoleAssignmentInput[];
		onSubmit: (data: { user: UserSubmission; isEditMode: boolean; userId?: string }) => void;
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
	void open;

	let isEditMode = $derived(!!userToEdit);
	let canEditUsername = $derived(!isEditMode || allowUsernameEdit);

	let isOidcUser = $derived(!!userToEdit?.oidcSubjectId);
	let hasOidcAssignments = $derived(!!userToEdit?.roleAssignments?.some((a) => a.source === 'oidc'));
	let assignmentsDisabled = $derived(isOidcUser && hasOidcAssignments);

	const formSchema = z.object({
		username: z.string().min(1, m.common_username_required()),
		password: z.string().optional(),
		displayName: z.string().optional(),
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
			.min(1, m.users_role_assignments_required())
	});

	let formData = $derived({
		username: userToEdit?.username || '',
		password: '',
		displayName: userToEdit?.displayName || '',
		email: userToEdit?.email || '',
		roleAssignments: availableRoleAssignments
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		// For OIDC users, only allow role assignment changes
		if (isOidcUser) {
			onSubmit({
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

		// Only include username if we're creating a new user or if editing is allowed
		if (!isEditMode || canEditUsername) {
			userData.username = data.username;
		}

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
				placeholder={m.users_email_placeholder()}
				description={m.users_email_description()}
				autocomplete="email"
				disabled={isOidcUser}
				bind:input={$inputs.email}
			/>
			<IfPermitted adminOnly>
				<div>
					<label for="roleAssignments" class="text-sm font-medium">{m.users_role_assignments_label()}</label>
					<p class="text-muted-foreground mb-2 text-xs">{m.users_role_assignments_description()}</p>
					{#if assignmentsDisabled}
						<Alert.Root variant="default" class="mb-3">
							<Alert.Description>
								{m.users_role_assignments_oidc_managed()}
								<a href="/settings/authentication" class="text-primary ml-2 underline">{m.users_role_assignments_oidc_link()}</a>
							</Alert.Description>
						</Alert.Root>
					{/if}
					<RoleAssignmentsEditor
						bind:assignments={$inputs.roleAssignments.value}
						{roles}
						{environments}
						disabled={assignmentsDisabled}
					/>
					{#if $inputs.roleAssignments.error}
						<p class="text-destructive mt-1 text-xs">{$inputs.roleAssignments.error}</p>
					{/if}
				</div>
			</IfPermitted>
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
