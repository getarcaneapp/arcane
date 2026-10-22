<script lang="ts">
	import { z } from 'zod/v4';

	import FormInput from '#lib/components/form/form-input.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Group, GroupCreate } from '#lib/types/auth.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	let {
		open = $bindable(false),
		groupToEdit,
		isLoading,
		onSubmit
	}: {
		open: boolean;
		groupToEdit: Group | null;
		isLoading: boolean;
		onSubmit: (data: Required<GroupCreate>) => void | Promise<void>;
	} = $props();

	const isEditMode = $derived(!!groupToEdit);

	const formSchema = z.object({
		name: z.string().trim().min(1, m.common_name_required()).max(100),
		description: z.string().trim().max(500)
	});

	const formData = $derived({
		name: groupToEdit?.name ?? '',
		description: groupToEdit?.description ?? ''
	});

	const form = $derived(createForm<typeof formSchema>(formSchema, formData));
	let inputs = $derived(form.inputs);

	async function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		await onSubmit({ name: data.name, description: data.description || null });
	}

	function handleOpenChange(next: boolean) {
		open = next;
		if (!next) form.reset();
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.groups_edit_title() : m.groups_create_title()}
	description={m.groups_form_description()}
	contentClass="sm:max-w-125"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} novalidate class="grid gap-4 py-6">
			<FormInput label={m.common_name()} type="text" disabled={isLoading} bind:input={inputs.name} />
			<FormInput label={m.common_description()} type="text" disabled={isLoading} bind:input={inputs.description} />
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
			submitLabel={isEditMode ? m.common_save() : m.common_create_button({ resource: m.resource_group_cap() })}
		/>
	{/snippet}
</ResponsiveDialog.Root>
