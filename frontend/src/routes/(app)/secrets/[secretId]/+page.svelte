<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { SecretsIcon, InfoIcon, ClockIcon } from '$lib/icons';
	import { goto } from '$app/navigation';
	import { openConfirmDialog } from '$lib/components/confirm-dialog/';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { format } from 'date-fns';
	import { m } from '$lib/paraglide/messages';
	import { untrack } from 'svelte';
	import { secretService } from '$lib/services/secret-service';
	import { ResourceDetailLayout, type DetailAction } from '$lib/layouts';
	import type { FormInput as FormInputType } from '$lib/utils/form.utils';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { z } from 'zod/v4';

	let { data } = $props();
	let secret = $state(untrack(() => data.secret));

	let originalName = $state(untrack(() => data.secret.name));
	let originalDescription = $state(untrack(() => data.secret.description ?? ''));
	let originalContent = $state(untrack(() => data.content ?? ''));

	let isLoading = $state({ save: false, remove: false });

	const createdDate = $derived(secret.createdAt ? format(new Date(secret.createdAt), 'PP p') : m.common_unknown());

	const formSchema = z.object({
		name: z.string().min(1, m.secret_name_required()),
		description: z.string().optional().default(''),
		content: z.string().min(1, m.common_required())
	});

	const initialSecret = untrack(() => data.secret);
	const initialContent = untrack(() => data.content ?? '');

	let nameInput = $state<FormInputType<string>>({
		value: initialSecret?.name ?? '',
		error: null
	});
	let descriptionInput = $state<FormInputType<string>>({
		value: initialSecret?.description ?? '',
		error: null
	});
	let contentInput = $state<FormInputType<string>>({
		value: initialContent,
		error: null
	});

	let hasChanges = $derived(
		nameInput.value !== originalName || descriptionInput.value !== originalDescription || contentInput.value !== originalContent
	);

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

	async function handleSave() {
		const validated = validateForm();
		if (!validated) return;

		const payload = {
			name: validated.name.trim(),
			description: validated.description?.trim() || undefined,
			content: validated.content
		};

		handleApiResultWithCallbacks({
			result: await tryCatch(secretService.updateSecret(secret.id, payload)),
			message: m.common_update_failed({ resource: `${m.resource_secret()} "${payload.name}"` }),
			setLoadingState: (value) => (isLoading.save = value),
			onSuccess: async (updated) => {
				toast.success(m.common_update_success({ resource: `${m.resource_secret()} "${payload.name}"` }));
				secret = updated;
				originalName = updated.name;
				originalDescription = updated.description ?? '';
				originalContent = payload.content;
				nameInput.value = updated.name;
				descriptionInput.value = updated.description ?? '';
			}
		});
	}

	async function handleRemoveSecretConfirm() {
		const safeName = secret.name?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_secret() }),
			message: m.common_remove_confirm({ resource: `${m.resource_secret()} "${safeName}"` }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(secretService.deleteSecret(secret.id)),
						message: m.common_remove_failed({ resource: `${m.resource_secret()} "${safeName}"` }),
						setLoadingState: (value) => (isLoading.remove = value),
						onSuccess: async () => {
							toast.success(m.common_remove_success({ resource: `${m.resource_secret()} "${safeName}"` }));
							goto('/secrets');
						}
					});
				}
			}
		});
	}

	const actions: DetailAction[] = $derived([
		{
			id: 'save',
			action: 'save',
			label: m.common_save(),
			loading: isLoading.save,
			disabled: isLoading.save || !hasChanges,
			onclick: handleSave
		},
		{
			id: 'remove',
			action: 'remove',
			label: m.common_remove(),
			loading: isLoading.remove,
			disabled: isLoading.remove,
			onclick: handleRemoveSecretConfirm
		}
	]);
</script>

{#if secret}
	<ResourceDetailLayout
		backUrl="/secrets"
		backLabel={m.secrets_title()}
		title={secret.name}
		subtitle={m.edit_secret_title()}
		{actions}
	>
		<div class="space-y-6">
			<Card.Root>
				<Card.Header icon={InfoIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>{m.common_details_title({ resource: m.resource_secret_cap() })}</Card.Title>
						<Card.Description>{m.common_details_description({ resource: m.resource_secret() })}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="p-4">
					<div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
						<FormInput
							label={m.secret_name_label()}
							id="secret-name"
							placeholder={m.secret_name_placeholder()}
							disabled={isLoading.save || isLoading.remove}
							bind:input={nameInput}
						/>

						<FormInput
							label={m.secret_description_label()}
							id="secret-description"
							disabled={isLoading.save || isLoading.remove}
							bind:input={descriptionInput}
						/>

						<div class="flex items-start gap-3">
							<div class="flex size-10 shrink-0 items-center justify-center rounded-full bg-green-500/10 p-2">
								<ClockIcon class="size-5 text-green-500" />
							</div>
							<div class="min-w-0 flex-1">
								<p class="text-muted-foreground text-sm font-medium">{m.common_created()}</p>
								<p class="mt-1 text-sm font-semibold sm:text-base">{createdDate}</p>
							</div>
						</div>
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header icon={SecretsIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>{m.secret_content_label()}</Card.Title>
						<Card.Description>{m.secret_content_description()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="p-4">
					<FormInput
						label={m.secret_content_label()}
						description={m.secret_content_description()}
						id="secret-content"
						disabled={isLoading.save || isLoading.remove}
						type="textarea"
						rows={10}
						bind:input={contentInput}
					/>
				</Card.Content>
			</Card.Root>
		</div>
	</ResourceDetailLayout>
{:else}
	<div class="flex flex-col items-center justify-center px-4 py-16 text-center">
		<div class="bg-muted/30 mb-4 rounded-full p-4">
			<SecretsIcon class="text-muted-foreground size-10 opacity-70" />
		</div>
		<h2 class="mb-2 text-xl font-medium">{m.common_not_found_title({ resource: m.secrets_title() })}</h2>
		<p class="text-muted-foreground mb-6">{m.common_not_found_description({ resource: m.secrets_title().toLowerCase() })}</p>

		<ArcaneButton
			action="cancel"
			customLabel={m.common_back_to({ resource: m.secrets_title() })}
			onclick={() => goto('/secrets')}
			size="sm"
		/>
	</div>
{/if}
