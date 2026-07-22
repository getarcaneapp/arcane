<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import SheetFooterActions from '$lib/components/sheets/sheet-footer-actions.svelte';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import type {
		ContainerRegistry,
		ContainerRegistryCreateDto,
		ContainerRegistryUpdateDto,
		RegistryType
	} from '$lib/types/docker';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
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
		{ value: 'ecr', label: m.amazon_ecr() }
	];

	const formSchema = z
		.object({
			registryType: z.enum(['generic', 'ecr']).default('generic'),
			url: z.string().min(1, m.registry_url_is_required()),
			username: z.string().optional(),
			token: z.string().optional(),
			description: z.string().optional(),
			repositoryNames: z.string().optional(),
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
			} else {
				if (!data.username?.trim()) {
					ctx.addIssue({
						code: z.ZodIssueCode.custom,
						message: m.common_username_required(),
						path: ['username']
					});
				}
				if (!isEditMode && !data.token?.trim()) {
					ctx.addIssue({
						code: z.ZodIssueCode.custom,
						message: m.registries_token_required(),
						path: ['token']
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
		repositoryNames: open && registryToEdit ? (registryToEdit.repositoryNames?.join('\n') ?? '') : '',
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
		const repositoryNames = data.repositoryNames
			? data.repositoryNames
					.split('\n')
					.map((s) => s.trim())
					.filter((s) => s.length > 0)
			: [];
		onSubmit({ registry: { ...data, repositoryNames }, isEditMode });
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
	title={isEditMode ? m.registries_edit_title() : m.common_add_button({ resource: m.common_registry() })}
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
				disabled={isEditMode}
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
					label={m.common_token()}
					type="password"
					placeholder={isEditMode ? m.registries_token_keep_placeholder() : m.registries_token_placeholder()}
					description={m.registries_token_description()}
					bind:input={$inputs.token}
				/>
			{:else}
				<FormInput label={m.registries_aws_access_key_id()} type="text" bind:input={$inputs.awsAccessKeyId} />
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
			<FormInput
				label={m.registries_repository_names()}
				type="textarea"
				rows={3}
				placeholder={m.registries_repository_names_placeholder()}
				description={m.registries_repository_names_description()}
				bind:input={$inputs.repositoryNames}
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
			submitLabel={isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.common_registry() })}
		/>
	{/snippet}
</ResponsiveDialog.Root>
