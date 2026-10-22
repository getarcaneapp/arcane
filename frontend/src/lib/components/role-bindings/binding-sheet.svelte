<script lang="ts">
	import { toast } from 'svelte-sonner';

	import RoleScopeSelects from '#lib/components/sheets/role-scope-selects.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { BindingSubjectType, Role, RoleBinding } from '#lib/types/auth.js';
	import type { Environment } from '#lib/types/environment.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import {
		buildGlobalEnvironmentOptions,
		createRoleEnvironmentLabelers,
		GLOBAL_ENVIRONMENT_OPTION_ID
	} from '#lib/utils/options.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		subjectType,
		subjectId,
		subjectName,
		roles,
		environments,
		onCreated
	}: {
		open: boolean;
		subjectType: BindingSubjectType;
		subjectId: string;
		subjectName: string;
		roles: Role[];
		environments: Environment[];
		onCreated: (binding: RoleBinding) => void | Promise<void>;
	} = $props();

	let roleId = $state('');
	let environmentValue = $state(GLOBAL_ENVIRONMENT_OPTION_ID);
	let roleError = $state<string | null>(null);
	let isSaving = $state(false);

	const envOptions = $derived(buildGlobalEnvironmentOptions(environments, m.global_org_wide()));
	const selectedLabel = $derived(createRoleEnvironmentLabelers(roles, envOptions, m.common_select_option()));
	const selectedRole = $derived(roles.find((role) => role.id === roleId));
	const environmentId = $derived(environmentValue === GLOBAL_ENVIRONMENT_OPTION_ID ? undefined : environmentValue);

	function reset() {
		roleId = '';
		environmentValue = GLOBAL_ENVIRONMENT_OPTION_ID;
		roleError = null;
	}

	function handleOpenChange(next: boolean) {
		open = next;
		if (!next) reset();
	}

	async function handleSubmit() {
		if (!selectedRole) {
			roleError = m.bindings_role_required();
			return;
		}
		roleError = null;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				roleService.createBinding({
					subjectType,
					subjectId,
					roleId: selectedRole.id,
					environmentId
				})
			),
			message: m.common_create_failed({ resource: m.resource_binding() }),
			setLoadingState: (value) => (isSaving = value),
			onSuccess: async (binding) => {
				toast.success(m.common_create_success({ resource: m.resource_binding_cap() }));
				handleOpenChange(false);
				await onCreated(binding);
			}
		});
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.bindings_create_title()}
	description={m.bindings_create_description({ subject: subjectName })}
	contentClass="sm:max-w-160"
>
	{#snippet children()}
		<div class="grid gap-6 py-6">
			<RoleScopeSelects
				idPrefix="binding"
				roleLabel={m.common_role()}
				scopeLabel={m.bindings_scope_label()}
				{roles}
				{envOptions}
				bind:roleValue={roleId}
				bind:environmentValue
				{roleError}
				roleSelectedLabel={selectedLabel.role}
				envSelectedLabel={selectedLabel.environment}
				disabled={isSaving}
			/>
		</div>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			onCancel={() => handleOpenChange(false)}
			cancelDisabled={isSaving}
			submitAction="create"
			submitDisabled={isSaving}
			submitLoading={isSaving}
			onSubmit={handleSubmit}
			submitLabel={m.bindings_add()}
		/>
	{/snippet}
</ResponsiveDialog.Root>
