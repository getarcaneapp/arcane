<script lang="ts">
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import KeyIcon from '@lucide/svelte/icons/key';
	import SaveIcon from '@lucide/svelte/icons/save';
	import type { ApiKey } from '$lib/types/api-key.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import * as m from '$lib/paraglide/messages.js';

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
	let SubmitIcon = $derived(isEditMode ? SaveIcon : KeyIcon);

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

<Sheet.Root bind:open onOpenChange={handleOpenChange}>
	<Sheet.Content class="p-6">
		<Sheet.Header class="space-y-3 border-b pb-6">
			<div class="flex items-center gap-3">
				<div class="bg-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg">
					<SubmitIcon class="text-primary size-5" />
				</div>
				<div>
					<Sheet.Title class="text-xl font-semibold">
						{isEditMode ? m.api_key_edit_title() : m.api_key_create_title()}
					</Sheet.Title>
					<Sheet.Description class="text-muted-foreground mt-1 text-sm">
						{#if isEditMode}
							{m.api_key_edit_description({ name: apiKeyToEdit?.name ?? m.common_unknown() })}
						{:else}
							{m.api_key_create_description()}
						{/if}
					</Sheet.Description>
				</div>
			</div>
		</Sheet.Header>

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

			<Sheet.Footer class="flex flex-row gap-2">
				<Button type="button" class="arcane-button-cancel flex-1" variant="outline" onclick={() => (open = false)} disabled={isLoading}
					>{m.common_cancel()}</Button
				>
				<Button type="submit" class="arcane-button-create flex-1" disabled={isLoading}>
					{#if isLoading}
						<Spinner class="mr-2 size-4" />
					{/if}
					<SubmitIcon class="mr-2 size-4" />
					{isEditMode ? m.api_key_save_changes() : m.api_key_create_button()}
				</Button>
			</Sheet.Footer>
		</form>
	</Sheet.Content>
</Sheet.Root>
