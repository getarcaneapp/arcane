<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { z } from 'zod/v4';
	import type { FormInput as FormInputType } from '$lib/utils/form.utils';
	import { preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import type { SecretCreateRequest } from '$lib/types/secret.type';

	type CreateSecretSheetProps = {
		open: boolean;
		onSubmit: (data: SecretCreateRequest) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), onSubmit, isLoading }: CreateSecretSheetProps = $props();

	const formSchema = z.object({
		name: z.string().min(1, m.secret_name_required()),
		content: z.string().min(1, m.common_required()),
		description: z.string().optional().default('')
	});

	let nameInput = $state<FormInputType<string>>({ value: '', error: null });
	let descriptionInput = $state<FormInputType<string>>({ value: '', error: null });
	let contentInput = $state<FormInputType<string>>({ value: '', error: null });

	function validateForm() {
		const result = formSchema.safeParse({
			name: nameInput.value,
			description: descriptionInput.value,
			content: contentInput.value
		});

		if (!result.success) {
			nameInput.error = result.error.issues.find((issue) => issue.path[0] === 'name')?.message ?? null;
			descriptionInput.error = result.error.issues.find((issue) => issue.path[0] === 'description')?.message ?? null;
			contentInput.error = result.error.issues.find((issue) => issue.path[0] === 'content')?.message ?? null;
			return null;
		}

		nameInput.error = null;
		descriptionInput.error = null;
		contentInput.error = null;
		return result.data;
	}

	function handleSubmit() {
		const data = validateForm();
		if (!data) return;

		const payload: SecretCreateRequest = {
			name: data.name.trim(),
			content: data.content,
			description: data.description?.trim() || undefined
		};

		onSubmit(payload);
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			nameInput.value = '';
			descriptionInput.value = '';
			contentInput.value = '';
		}
	}
</script>

{#snippet dialogContent()}
	<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
		<FormInput
			label={m.secret_name_label()}
			id="secret-name"
			type="text"
			placeholder={m.secret_name_placeholder()}
			disabled={isLoading}
			bind:input={nameInput}
		/>

		<FormInput label={m.secret_description_label()} id="secret-description" disabled={isLoading} bind:input={descriptionInput} />

		<FormInput
			label={m.secret_content_label()}
			description={m.secret_content_description()}
			id="secret-content"
			disabled={isLoading}
			type="textarea"
			rows={8}
			bind:input={contentInput}
		/>
	</form>
{/snippet}

{#snippet dialogFooter()}
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
			customLabel={m.common_create_button({ resource: m.resource_secret_cap() })}
		/>
	</div>
{/snippet}

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.create_secret_title()}
	description={m.secrets_subtitle()}
	contentClass="sm:max-w-[720px]"
	children={dialogContent}
	footer={dialogFooter}
/>
