<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import * as Select from '$lib/components/ui/select';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { Label } from '$lib/components/ui/label';
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

	const envOptions = $derived(buildGlobalEnvironmentOptions(environments, m.oidc_mappings_scope_global_option()));
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
				label={m.oidc_mappings_claim_label()}
				type="text"
				placeholder={m.oidc_mappings_claim_placeholder()}
				disabled={isLoading}
				bind:input={$inputs.claimValue}
			/>

			<div class="space-y-2">
				<Label for="oidc-mapping-role" class="mb-0">{m.oidc_mappings_role_label()}</Label>
				<Select.Root type="single" bind:value={$inputs.roleId.value} disabled={isLoading}>
					<Select.Trigger id="oidc-mapping-role" class="w-full {$inputs.roleId.error ? 'border-destructive' : ''}">
						<span>{selectedLabel.role($inputs.roleId.value)}</span>
					</Select.Trigger>
					<Select.Content>
						{#each roles as role (role.id)}
							<Select.Item value={role.id} label={role.name}>
								<div class="flex flex-col items-start gap-0.5">
									<span class="font-medium">{role.name}</span>
									{#if role.description}
										<span class="text-muted-foreground text-xs">{role.description}</span>
									{/if}
								</div>
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
				{#if $inputs.roleId.error}
					<p class="text-destructive text-xs font-medium">{$inputs.roleId.error}</p>
				{/if}
			</div>

			<div class="space-y-2">
				<Label for="oidc-mapping-env" class="mb-0">{m.oidc_mappings_scope_label()}</Label>
				<Select.Root type="single" bind:value={$inputs.environmentId.value} disabled={isLoading}>
					<Select.Trigger id="oidc-mapping-env" class="w-full">
						<span>{selectedLabel.environment($inputs.environmentId.value)}</span>
					</Select.Trigger>
					<Select.Content>
						{#each envOptions as option (option.id)}
							<Select.Item value={option.id} label={option.name}>
								{option.name}
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
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
				customLabel={isEditMode ? m.common_save() : m.common_create()}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
