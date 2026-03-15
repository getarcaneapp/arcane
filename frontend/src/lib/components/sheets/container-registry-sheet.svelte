<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import type { ContainerRegistry, ContainerRegistryCreateDto, ContainerRegistryUpdateDto, RegistryType } from '$lib/types/container-registry.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';

	type ContainerRegistryFormProps = {
		open: boolean;
		registryToEdit: ContainerRegistry | null;
		onSubmit: (detail: { registry: ContainerRegistryCreateDto | ContainerRegistryUpdateDto; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), registryToEdit = $bindable(), onSubmit, isLoading }: ContainerRegistryFormProps = $props();

	let isEditMode = $derived(!!registryToEdit);

	const registryTypeOptions = [
		{ value: 'generic', label: m.registries_type_generic() },
		{ value: 'ecr', label: m.registries_type_ecr() }
	];

	const formSchema = z
		.object({
			registryType: z.enum(['generic', 'ecr']).default('generic'),
			url: z.string().min(1, m.registries_url_required()),
			username: z.string().optional(),
			token: z.string().optional(),
			description: z.string().optional(),
			insecure: z.boolean().default(false),
			enabled: z.boolean().default(true),
			awsAccessKeyId: z.string().optional(),
			awsSecretAccessKey: z.string().optional(),
			awsRegion: z.string().optional()
		})
		.superRefine((data, ctx) => {
			if (data.registryType === 'ecr') {
				if (!data.awsAccessKeyId?.trim()) {
					ctx.addIssue({
						code: z.ZodIssueCode.custom,
						message: m.registries_aws_access_key_id_required(),
						path: ['awsAccessKeyId']
					});
				}
				if (!data.awsRegion?.trim()) {
					ctx.addIssue({
						code: z.ZodIssueCode.custom,
						message: m.registries_aws_region_required(),
						path: ['awsRegion']
					});
				}
				if (!isEditMode && !data.awsSecretAccessKey?.trim()) {
					ctx.addIssue({
						code: z.ZodIssueCode.custom,
						message: m.registries_aws_secret_access_key_required(),
						path: ['awsSecretAccessKey']
					});
				}
			}
		});

	let formData = $derived({
		registryType: (open && registryToEdit ? (registryToEdit.registryType ?? 'generic') : 'generic') as RegistryType,
		url: open && registryToEdit ? registryToEdit.url : '',
		username: open && registryToEdit ? registryToEdit.username : '',
		token: '',
		description: open && registryToEdit ? registryToEdit.description || '' : '',
		insecure: open && registryToEdit ? (registryToEdit.insecure ?? false) : false,
		enabled: open && registryToEdit ? (registryToEdit.enabled ?? true) : true,
		awsAccessKeyId: open && registryToEdit ? (registryToEdit.awsAccessKeyId ?? '') : '',
		awsSecretAccessKey: '',
		awsRegion: open && registryToEdit ? (registryToEdit.awsRegion ?? '') : ''
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let isECR = $derived($inputs.registryType.value === 'ecr');

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		onSubmit({ registry: data, isEditMode });
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			registryToEdit = null;
		}
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.registries_edit_title() : m.common_add_button({ resource: m.resource_registry_cap() })}
	description={isEditMode ? m.registries_edit_description() : m.registries_add_description()}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<SelectWithLabel
				id="registryTypeSelect"
				label={m.registries_type_label()}
				options={registryTypeOptions}
				bind:value={$inputs.registryType.value}
			/>

			<FormInput
				label={m.registries_url()}
				type="text"
				placeholder={m.registries_url_placeholder()}
				description={m.registries_url_description()}
				bind:input={$inputs.url}
			/>

			{#if !isECR}
				<FormInput
					label={m.common_username()}
					type="text"
					description={m.common_username_required()}
					bind:input={$inputs.username}
				/>
				<FormInput
					label={m.registries_token_label()}
					type="password"
					placeholder={isEditMode ? m.registries_token_keep_placeholder() : m.registries_token_placeholder()}
					description={m.registries_token_description()}
					bind:input={$inputs.token}
				/>
			{:else}
				<FormInput
					label={m.registries_aws_access_key_id()}
					type="text"
					bind:input={$inputs.awsAccessKeyId}
				/>
				<FormInput
					label={m.registries_aws_secret_access_key()}
					type="password"
					placeholder={isEditMode ? m.registries_token_keep_placeholder() : ''}
					bind:input={$inputs.awsSecretAccessKey}
				/>
				<FormInput
					label={m.registries_aws_region()}
					type="text"
					placeholder={m.registries_aws_region_placeholder()}
					bind:input={$inputs.awsRegion}
				/>
			{/if}

			<FormInput
				label={m.common_description()}
				type="text"
				placeholder={m.registries_description_placeholder()}
				bind:input={$inputs.description}
			/>
			<SwitchWithLabel
				id="isEnabledSwitch"
				label={m.common_enabled()}
				description={m.registries_enabled_description()}
				bind:checked={$inputs.enabled.value}
			/>
			{#if !isECR}
				<SwitchWithLabel
					id="insecureSwitch"
					label={m.registries_allow_insecure_label()}
					description={m.registries_allow_insecure_description()}
					bind:checked={$inputs.insecure.value}
				/>
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
				action={isEditMode ? 'save' : 'create'}
				type="submit"
				class="flex-1"
				disabled={isLoading}
				loading={isLoading}
				onclick={handleSubmit}
				customLabel={isEditMode ? m.registries_save_changes() : m.common_add_button({ resource: m.resource_registry_cap() })}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
