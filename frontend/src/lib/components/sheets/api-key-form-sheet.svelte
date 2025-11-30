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
		name: z.string().min(1, 'Name is required'),
		description: z.string().optional(),
		expiresAt: z.string().optional()
	});

	let formData = $derived({
		name: apiKeyToEdit?.name || '',
		description: apiKeyToEdit?.description || '',
		expiresAt: apiKeyToEdit?.expiresAt ? apiKeyToEdit.expiresAt.slice(0, 10) : ''
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		const apiKeyData = {
			name: data.name,
			description: data.description || undefined,
			expiresAt: data.expiresAt ? new Date(data.expiresAt).toISOString() : undefined
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
						{isEditMode ? 'Edit API Key' : 'Create API Key'}
					</Sheet.Title>
					<Sheet.Description class="text-muted-foreground mt-1 text-sm">
						{#if isEditMode}
							Update the details of the API key "{apiKeyToEdit?.name ?? 'Unknown'}"
						{:else}
							Create a new API key for programmatic access
						{/if}
					</Sheet.Description>
				</div>
			</div>
		</Sheet.Header>

		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput
				label="Name"
				type="text"
				placeholder="Enter a name for this API key"
				description="A friendly name to identify this API key"
				bind:input={$inputs.name}
			/>
			<FormInput
				label="Description"
				type="text"
				placeholder="Optional description"
				description="Additional details about what this API key is used for"
				bind:input={$inputs.description}
			/>
			<FormInput
				label="Expires At"
				type="date"
				placeholder="Optional expiration date"
				description="Leave empty for a non-expiring key"
				bind:input={$inputs.expiresAt}
			/>

			<Sheet.Footer class="flex flex-row gap-2">
				<Button type="button" class="arcane-button-cancel flex-1" variant="outline" onclick={() => (open = false)} disabled={isLoading}
					>Cancel</Button
				>
				<Button type="submit" class="arcane-button-create flex-1" disabled={isLoading}>
					{#if isLoading}
						<Spinner class="mr-2 size-4" />
					{/if}
					<SubmitIcon class="mr-2 size-4" />
					{isEditMode ? 'Save Changes' : 'Create API Key'}
				</Button>
			</Sheet.Footer>
		</form>
	</Sheet.Content>
</Sheet.Root>
