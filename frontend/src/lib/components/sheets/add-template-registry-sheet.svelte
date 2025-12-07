<script lang="ts">
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import GlobeIcon from 'phosphor-svelte/lib/Globe';
	import AlertCircleIcon from 'phosphor-svelte/lib/Warning';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';

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

<Sheet.Root bind:open onOpenChange={handleOpenChange}>
	<Sheet.Content class="p-6">
		<Sheet.Header class="space-y-3 border-b pb-6">
			<div class="flex items-center gap-3">
				<div class="bg-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg">
					<GlobeIcon class="text-primary size-5" />
				</div>
				<div>
					<Sheet.Title class="text-xl font-semibold">{m.templates_add_registry_title()}</Sheet.Title>
					<Sheet.Description class="text-muted-foreground mt-1 text-sm">
						{m.templates_add_registry_description()}
					</Sheet.Description>
				</div>
			</div>
		</Sheet.Header>
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-4">
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
					<AlertCircleIcon class="size-4" />
					<Alert.Title>{m.templates_registry_validation_error_title()}</Alert.Title>
					<Alert.Description class="text-sm">{submitError}</Alert.Description>
				</Alert.Root>
			{/if}

			<Sheet.Footer class="flex flex-row gap-2">
				<Button
					type="button"
					class="arcane-button-cancel flex-1"
					variant="outline"
					onclick={() => (open = false)}
					disabled={isLoading}
				>
					{m.common_cancel()}
				</Button>
				<Button type="submit" class="arcane-button-create flex-1" disabled={isLoading}>
					{#if isLoading}
						<Spinner class="mr-2 size-4" />
					{/if}
					{m.templates_add_registry_button()}
				</Button>
			</Sheet.Footer>
		</form>
	</Sheet.Content>
</Sheet.Root>
