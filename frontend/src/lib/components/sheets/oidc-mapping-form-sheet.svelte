<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import SheetFooterActions from '$lib/components/sheets/sheet-footer-actions.svelte';
	import RoleScopeSelects from '$lib/components/sheets/role-scope-selects.svelte';
	import FormInput from '$lib/components/form/form-input.svelte';
	import type { OidcRoleMapping, Role } from '$lib/types/auth';
	import type { Environment } from '$lib/types/environment';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import { m } from '$lib/paraglide/messages';
	import { buildGlobalEnvironmentOptions, createRoleEnvironmentLabelers, GLOBAL_ENVIRONMENT_OPTION_ID } from '$lib/utils/options';

	type Props = {
		open: boolean;
		mappingToEdit: OidcRoleMapping | null;
		roles: Role[];
		environments: Environment[];
		isLoading: boolean;
		onSubmit: (data: { claimValue: string; roleId: string; environmentId?: string }) => void;
	};

	let { open = $bindable(false), mappingToEdit, roles, environments, isLoading, onSubmit }: Props = $props();

	const isEditMode = $derived(!!mappingToEdit);

	const envOptions = $derived(buildGlobalEnvironmentOptions(environments, m.global_org_wide()));
	const selectedLabel = $derived(createRoleEnvironmentLabelers(roles, envOptions, m.common_select_option()));

	const formSchema = z.object({
		claimValue: z.string().min(1, m.oidc_mappings_claim_required()),
		roleId: z.string().min(1, m.oidc_mappings_role_required()),
		environmentId: z.string()
	});

	const formData = $derived({
		claimValue: mappingToEdit?.claimValue ?? '',
		roleId: mappingToEdit?.roleId ?? roles[0]?.id ?? '',
		environmentId: mappingToEdit?.environmentId ?? GLOBAL_ENVIRONMENT_OPTION_ID
	});

	const { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		onSubmit({
			claimValue: data.claimValue,
			roleId: data.roleId,
			environmentId: data.environmentId === GLOBAL_ENVIRONMENT_OPTION_ID ? undefined : data.environmentId
		});
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.oidc_mappings_edit_title() : m.oidc_mappings_create_title()}
	description={m.oidc_mappings_subtitle()}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} novalidate class="grid gap-4 py-6">
			<FormInput
				label={m.claim_value()}
				type="text"
				placeholder={m.oidc_mappings_claim_placeholder()}
				disabled={isLoading}
				bind:input={$inputs.claimValue}
			/>

			<RoleScopeSelects
				idPrefix="oidc-mapping"
				roleLabel={m.common_role()}
				scopeLabel={m.oidc_mappings_scope_label()}
				{roles}
				{envOptions}
				bind:roleValue={$inputs.roleId.value}
				bind:environmentValue={$inputs.environmentId.value}
				roleError={$inputs.roleId.error}
				roleSelectedLabel={selectedLabel.role}
				envSelectedLabel={selectedLabel.environment}
				disabled={isLoading}
			/>
			<!-- fallow-ignore-next-line code-duplication -- per-sheet footer wrapper ({#snippet footer} -> shared SheetFooterActions); ResponsiveDialog requires a footer snippet in each sheet -->
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={isLoading}
			submitAction={isEditMode ? 'save' : 'create'}
			submitDisabled={isLoading}
			submitLoading={isLoading}
			onSubmit={handleSubmit}
			submitLabel={isEditMode ? m.common_save() : m.common_create()}
		/>
	{/snippet}
</ResponsiveDialog.Root>
