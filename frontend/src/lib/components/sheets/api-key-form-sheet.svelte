<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import type { ApiKey } from '$lib/types/api-key.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import * as m from '$lib/paraglide/messages.js';
	import { ApiKeyIcon, SaveIcon } from '$lib/icons';

	type ApiKeyFormProps = {
		open: boolean;
		apiKeyToEdit: ApiKey | null;
		onSubmit: (data: {
			apiKey: { name: string; description?: string; expiresAt?: string };
			isEditMode: boolean;
			apiKeyId?: string;
		}) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), apiKeyToEdit = $bindable(), onSubmit, isLoading }: ApiKeyFormProps = $props();

	let isEditMode = $derived(!!apiKeyToEdit);
	let SubmitIcon = $derived(isEditMode ? SaveIcon : ApiKeyIcon);

	const formSchema = z.object({
		name: z.string().min(1, m.common_field_required({ field: m.api_key_name() })),
		description: z.string().optional(),
		expiresAt: z.date().optional()
	});

	let formData = $derived({
		name: apiKeyToEdit?.name || '',
		description: apiKeyToEdit?.description || '',
		expiresAt: apiKeyToEdit?.expiresAt ? new Date(apiKeyToEdit.expiresAt) : undefined
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		const apiKeyData = {
			name: data.name,
			description: data.description || undefined,
			expiresAt: data.expiresAt ? data.expiresAt.toISOString() : undefined
		};

		onSubmit({ apiKey: apiKeyData, isEditMode, apiKeyId: apiKeyToEdit?.id });
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			apiKeyToEdit = null;
		}
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.api_key_edit_title() : m.api_key_create_title()}
	description={isEditMode
		? m.api_key_edit_description({ name: apiKeyToEdit?.name ?? m.common_unknown() })
		: m.api_key_create_description()}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput
				label={m.api_key_name()}
				type="text"
				placeholder={m.api_key_name_placeholder()}
				description={m.api_key_name_description()}
				bind:input={$inputs.name}
			/>
			<FormInput
				label={m.api_key_description_label()}
				type="text"
				placeholder={m.api_key_description_placeholder()}
				description={m.api_key_description_help()}
				bind:input={$inputs.description}
			/>
			<FormInput
				label={m.api_key_expires_at()}
				type="date"
				description={m.api_key_expires_at_description()}
				bind:input={$inputs.expiresAt}
			/>
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
				customLabel={isEditMode ? m.api_key_save_changes() : m.api_key_create_button()}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
