<script lang="ts">
	import { resolve } from '$app/paths';
	import { untrack } from 'svelte';
	import { z } from 'zod/v4';

	import FormInput from '#lib/components/form/form-input.svelte';
	import RoleAssignmentsEditor from '#lib/components/forms/role-assignments-editor.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { Environment } from '#lib/types/environment.js';
	import type { Role, UserRoleAssignmentInput } from '#lib/types/role.js';
	import type { UserUpdate } from '#lib/types/ui/user.js';
	import type { User } from '#lib/types/user.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	type RoleAssignmentInput = UserRoleAssignmentInput;
	type UserSubmission = UserUpdate & {
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
		userToEdit,
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
	const isDirectoryUser = $derived(!!userToEdit?.ldapId && settingsStore.current?.ldapEnabled === true);
	const isExternallyManaged = $derived(isOidcUser || isDirectoryUser);
	const sheetDescription = $derived.by(() => {
		if (isDirectoryUser) return m.directory_manages_user();
		if (isEditMode) return m.users_edit_description({ username: userToEdit?.username ?? m.common_unknown() });
		return m.users_create_description();
	});
	const passwordPlaceholder = $derived.by(() => {
		if (isExternallyManaged) return m.users_password_managed_oidc();
		if (isEditMode) return m.users_password_leave_empty();
		return m.users_password_enter();
	});
	const passwordDescription = $derived.by(() => {
		if (isOidcUser) return m.users_password_description_oidc();
		if (isDirectoryUser) return undefined;
		if (isEditMode) return m.users_password_description_edit();
		return m.users_password_description_create();
	});
	let oidcAssignments = $derived(userToEdit?.roleAssignments?.filter((a) => a.source !== 'manual') ?? []);
	let userGroups = $derived(userToEdit?.groups ?? []);
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
		email: z.string().trim(),
		roleAssignments: z
			.array(
				z.object({
					roleId: z.string().min(1),
					environmentId: z.string().optional(),
					resourceType: z.enum(['project', 'container', 'volume', 'network', 'image', 'gitops']).optional(),
					resourceId: z.string().optional()
				})
			)
			.refine(
				(assignments) => isExternallyManaged || userGroups.length > 0 || assignments.length >= 1,
				m.users_role_assignments_required()
			)
	});

	const form = createForm<typeof formSchema>(
		formSchema,
		untrack(() => ({
			username: userToEdit?.username || '',
			password: '',
			displayName: userToEdit?.displayName || '',
			email: userToEdit?.email || '',
			roleAssignments: availableRoleAssignments
		}))
	);

	async function submitUser(data: UserFormSubmission) {
		if (await onSubmit(data)) {
			form.reset();
		}
	}

	async function handleSubmit() {
		const password = form.inputs.password.value;
		const data = form.validate();
		if (!data) return;

		if (isExternallyManaged) {
			await submitUser({
				user: { roleAssignments: data.roleAssignments },
				isEditMode,
				userId: userToEdit?.id
			});
			return;
		}

		if (!userToEdit) {
			await submitUser({
				user: {
					username: data.username,
					password,
					displayName: data.displayName || undefined,
					email: data.email || undefined,
					roleAssignments: data.roleAssignments
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
				...(password ? { password } : {}),
				roleAssignments: data.roleAssignments
			},
			isEditMode,
			userId: userToEdit.id
		});
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			form.reset();
		}
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.users_edit_title() : m.users_create_new_title()}
	description={sheetDescription}
	contentClass="sm:max-w-160"
>
	<form onsubmit={preventDefault(handleSubmit)} novalidate class="grid gap-4 py-6">
		<FormInput
			label={m.common_username()}
			type="text"
			description={m.users_username_description()}
			disabled={!canEditUsername || isExternallyManaged}
			bind:input={form.inputs.username}
		/>
		<FormInput
			label={isEditMode ? m.common_password() : m.users_password_required_label()}
			type="password"
			placeholder={passwordPlaceholder}
			description={passwordDescription}
			disabled={isExternallyManaged}
			bind:input={form.inputs.password}
		/>
		<FormInput
			label={m.common_display_name()}
			type="text"
			placeholder={m.users_display_name_placeholder()}
			description={m.users_display_name_description()}
			disabled={isExternallyManaged}
			bind:input={form.inputs.displayName}
		/>
		<FormInput
			label={m.common_email()}
			type="text"
			placeholder={m.user_example_com_placeholder()}
			description={m.users_email_description()}
			autocomplete="email"
			disabled={isExternallyManaged}
			bind:input={form.inputs.email}
		/>
		<IfPermitted adminOnly>
			<div>
				<label for="roleAssignments" class="text-sm font-medium">{m.users_role_assignments_label()}</label>
				<p class="mb-2 text-xs text-muted-foreground">{m.users_role_assignments_description()}</p>
				{#if userGroups.length > 0}
					<div class="mb-3">
						<p class="text-xs text-muted-foreground">
							{m.user_inherited_groups()}
						</p>
						<div class="mt-2 flex flex-wrap gap-2">
							{#each userGroups as group (`${group.id}-${group.source}`)}
								<a href={resolve('/(app)/system/groups/[id]', { id: group.id })}>
									<Badge variant="outline">{group.name}</Badge>
								</a>
							{/each}
						</div>
					</div>
				{/if}
				{#if oidcAssignments.length > 0}
					<div class="mb-3">
						<p class="text-xs text-muted-foreground">
							{m.oidc_synced_role_assignments()}
							<a href={resolve('/(app)/system/authentication')} class="ml-1 text-primary-text underline"
								>{m.users_role_assignments_oidc_link()}</a
							>
						</p>
						<div class="mt-2 flex flex-wrap gap-2">
							{#each oidcAssignments as assignment (`${assignment.source}-${assignment.roleId}-${assignment.environmentId ?? 'global'}`)}
								<Badge variant="outline">
									{roles.find((r) => r.id === assignment.roleId)?.name ?? assignment.roleId}
									·
									{environments.find((e) => e.id === assignment.environmentId)?.name ?? m.global_org_wide()}
								</Badge>
							{/each}
						</div>
					</div>
				{/if}
				<RoleAssignmentsEditor bind:assignments={form.inputs.roleAssignments.value} {roles} {environments} />
				{#if form.inputs.roleAssignments.error}
					<p class="mt-1 text-xs text-destructive">{form.inputs.roleAssignments.error}</p>
				{/if}
			</div>
		</IfPermitted>
		<!-- fallow-ignore-next-line code-duplication -- per-sheet footer wrapper ({#snippet footer} -> shared SheetFooterActions); ResponsiveDialog requires a footer snippet in each sheet -->
	</form>

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
