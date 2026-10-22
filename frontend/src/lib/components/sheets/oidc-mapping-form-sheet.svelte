<script lang="ts">
	import { z } from 'zod/v4';

	import FormInput from '#lib/components/form/form-input.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Group, OidcGroupMapping, OidcGroupMappingSpec } from '#lib/types/auth.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	type Props = {
		open: boolean;
		mappingToEdit: OidcGroupMapping | null;
		groups: Group[];
		isLoading: boolean;
		onSubmit: (data: OidcGroupMappingSpec) => void;
	};

	let { open = $bindable(false), mappingToEdit, groups, isLoading, onSubmit }: Props = $props();

	const isEditMode = $derived(!!mappingToEdit);

	const formSchema = z.object({
		claimValue: z.string().min(1, m.oidc_mappings_claim_required()),
		groupId: z.string().min(1, m.oidc_mappings_group_required())
	});

	const formData = $derived({
		claimValue: mappingToEdit?.claimValue ?? '',
		groupId: mappingToEdit?.groupId ?? groups[0]?.id ?? ''
	});

	const form = $derived(createForm<typeof formSchema>(formSchema, formData));
	let inputs = $derived(form.inputs);

	const selectedGroupName = $derived(groups.find((group) => group.id === inputs.groupId.value)?.name ?? m.common_select_option());

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		onSubmit({ claimValue: data.claimValue, groupId: data.groupId });
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
	description={m.oidc_group_mappings_subtitle()}
	contentClass="sm:max-w-125"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} novalidate class="grid gap-4 py-6">
			<FormInput
				label={m.claim_value()}
				type="text"
				placeholder={m.oidc_mappings_claim_placeholder()}
				disabled={isLoading}
				bind:input={inputs.claimValue}
			/>

			<div class="space-y-2">
				<Label for="oidc-mapping-group" class="mb-0">{m.resource_group_cap()}</Label>
				<Select.Root type="single" bind:value={inputs.groupId.value} disabled={isLoading || groups.length === 0}>
					<Select.Trigger id="oidc-mapping-group" class="w-full" aria-invalid={!!inputs.groupId.error}>
						<span class="min-w-0 truncate">{selectedGroupName}</span>
					</Select.Trigger>
					<Select.Content>
						{#each groups as group (group.id)}
							<Select.Item value={group.id} label={group.name}>
								<span class="flex flex-col">
									<span>{group.name}</span>
									{#if group.description}
										<span class="text-xs text-muted-foreground">{group.description}</span>
									{/if}
								</span>
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
				{#if groups.length === 0}
					<p class="text-xs text-muted-foreground">
						{m.oidc_mappings_no_groups()}
						<a href="/settings/groups" class="ml-1 text-primary underline">{m.groups_title()}</a>
					</p>
				{/if}
				{#if inputs.groupId.error}
					<p class="text-xs text-destructive">{inputs.groupId.error}</p>
				{/if}
			</div>
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
