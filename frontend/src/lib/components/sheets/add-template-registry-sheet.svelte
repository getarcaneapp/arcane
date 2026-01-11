<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';
	import { AlertIcon } from '$lib/icons';

	type TemplateRegistryFormProps = {
		open: boolean;
		onSubmit: (registry: { name: string; url: string; description?: string; enabled: boolean }) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), onSubmit, isLoading }: TemplateRegistryFormProps = $props();

	const formSchema = z.object({
		url: z.url().min(1, m.templates_registry_url_required()),
		enabled: z.boolean().default(true)
	});

	let formData = $derived({
		url: '',
		enabled: true
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let submitError = $state<string | null>(null);

	async function handleSubmit() {
		submitError = null;

		const data = form.validate();
		if (!data) return;

		try {
			const registryData = await templateService.fetchRegistry(data.url);

			if (!registryData.name || !registryData.templates || !Array.isArray(registryData.templates)) {
				throw new Error(m.templates_registry_invalid_format());
			}

			const registryPayload = {
				name: registryData.name,
				url: data.url,
				description: registryData.description || '',
				enabled: data.enabled
			};

			onSubmit(registryPayload);
		} catch (error) {
			submitError = error instanceof Error ? error.message : m.templates_registry_validate_failed();
		}
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			submitError = null;
		}
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.templates_add_registry_title()}
	description={m.templates_add_registry_description()}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput
				label={m.templates_registry_url_label()}
				type="text"
				placeholder={m.templates_registry_url_placeholder()}
				description={m.templates_registry_url_description()}
				bind:input={$inputs.url}
			/>

			<SwitchWithLabel
				id="enabledSwitch"
				label={m.templates_enable_registry_label()}
				description={m.templates_enable_registry_description()}
				bind:checked={$inputs.enabled.value}
			/>

			{#if submitError}
				<Alert.Root class="border-red-200 bg-red-50 dark:border-red-800 dark:bg-red-950">
					<AlertIcon class="size-4" />
					<Alert.Title>{m.templates_registry_validation_error_title()}</Alert.Title>
					<Alert.Description class="text-sm">{submitError}</Alert.Description>
				</Alert.Root>
			{/if}
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
				action="create"
				type="submit"
				class="flex-1"
				disabled={isLoading}
				loading={isLoading}
				onclick={handleSubmit}
				customLabel={m.templates_add_registry_button()}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
