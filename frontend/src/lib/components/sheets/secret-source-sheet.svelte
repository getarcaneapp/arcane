<script lang="ts">
	import { untrack } from 'svelte';
	import { z } from 'zod/v4';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import FormInput from '#lib/components/form/form-input.svelte';
	import ProviderIcon from '#lib/components/secret-sources/provider-icon.svelte';
	import { endpointKey, sortedProviderOptions } from '#lib/components/secret-sources/providers.js';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { AlertIcon, CheckIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type {
		SecretSourceProvider,
		SecretSource,
		SecretSourceCreateRequest,
		SecretSourceTestResult,
		SecretSourceUpdateRequest,
		SecretSourceSettings
	} from '#lib/types/secretsource.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type SecretSourceFormPayload =
		| { mode: 'create'; source: SecretSourceCreateRequest }
		| { mode: 'edit'; id: string; source: SecretSourceUpdateRequest };

	let {
		open = $bindable(false),
		sourceToEdit = null,
		isLoading = false,
		onSubmit
	}: {
		open: boolean;
		sourceToEdit?: SecretSource | null;
		isLoading?: boolean;
		onSubmit: (payload: SecretSourceFormPayload) => void;
	} = $props();

	const isEditMode = $derived(!!sourceToEdit);
	const hasStoredSecret = $derived(!!sourceToEdit?.hasCredential);

	// The provider is fixed once a source exists; bindings depend on it.
	let provider = $state<SecretSourceProvider>(untrack(() => sourceToEdit?.provider ?? 'infisical'));

	const providers = sortedProviderOptions();

	// A stored credential is only reused while the address stays the same;
	// the backend refuses otherwise.
	const storedEndpoint = untrack(() => (sourceToEdit ? endpointKey(sourceToEdit.settings) : ''));

	const formSchema = untrack(() =>
		z
			.object({
				name: z.string().trim().min(1, m.common_name_required()),
				siteUrl: z.string().trim(),
				clientId: z.string().trim(),
				clientSecret: z.string(),
				organizationSlug: z.string().trim()
			})
			.superRefine((data, ctx) => {
				if (!data.clientId) {
					ctx.addIssue({ code: 'custom', path: ['clientId'], message: m.secret_sources_client_id_required() });
				}
				// Editing keeps the stored secret when the field is left empty,
				// unless the address changed.
				if (!sourceToEdit && data.clientSecret === '') {
					ctx.addIssue({ code: 'custom', path: ['clientSecret'], message: m.secret_sources_client_secret_required() });
				}
				const endpointChanged = !!sourceToEdit && endpointKey(currentSettings()) !== storedEndpoint;
				if (endpointChanged && hasStoredSecret && data.clientSecret === '') {
					ctx.addIssue({ code: 'custom', path: ['clientSecret'], message: m.secret_sources_credential_reenter() });
				}
			})
	);

	const form = createForm<typeof formSchema>(
		formSchema,
		untrack(() => ({
			name: sourceToEdit?.name ?? '',
			siteUrl: sourceToEdit?.settings.infisical?.siteUrl ?? '',
			clientId: sourceToEdit?.settings.infisical?.clientId ?? '',
			clientSecret: '',
			organizationSlug: sourceToEdit?.settings.infisical?.organizationSlug ?? ''
		}))
	);
	let inputs = $derived(form.inputs);

	let testing = $state(false);
	let testResult = $state<SecretSourceTestResult | null>(null);

	function currentSettings(): SecretSourceSettings {
		return {
			infisical: {
				siteUrl: inputs.siteUrl.value.trim(),
				clientId: inputs.clientId.value.trim(),
				organizationSlug: inputs.organizationSlug.value.trim()
			}
		};
	}

	function visibleLabel(result: SecretSourceTestResult): string {
		return result.visibleCount === 1
			? m.secret_sources_test_visible_projects_one()
			: m.secret_sources_test_visible_projects({ count: result.visibleCount });
	}

	async function testConnection() {
		testResult = null;
		if (!inputs.clientId.value.trim()) {
			inputs.clientId.error = m.secret_sources_client_id_required();
			return;
		}
		const endpointChanged = !!sourceToEdit && endpointKey(currentSettings()) !== storedEndpoint;
		if (!inputs.clientSecret.value && (!hasStoredSecret || endpointChanged)) {
			inputs.clientSecret.error = endpointChanged
				? m.secret_sources_credential_reenter()
				: m.secret_sources_client_secret_required();
			return;
		}

		testing = true;
		const result = await tryCatch(
			secretSourceService.test({
				sourceId: sourceToEdit?.id,
				provider,
				settings: currentSettings(),
				credential: inputs.clientSecret.value || undefined
			})
		);
		testing = false;
		testResult = result.error
			? { ok: false, message: extractApiErrorMessage(result.error), canBrowse: false, visibleCount: 0 }
			: result.data;
	}

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		const settings = currentSettings();
		const credential = data.clientSecret || undefined;
		if (isEditMode && sourceToEdit) {
			onSubmit({ mode: 'edit', id: sourceToEdit.id, source: { name: data.name, settings, credential } });
			return;
		}
		onSubmit({ mode: 'create', source: { name: data.name, provider, settings, credential } });
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={isEditMode ? m.secret_sources_edit_title() : m.secret_sources_add_title()}
	description={isEditMode ? m.common_edit_description() : m.secret_sources_add_description()}
	contentClass="sm:max-w-md"
>
	{#snippet children()}
		<form id="secret-source-form" onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<div class="space-y-2">
				<Label class="mb-0">{m.secret_sources_provider()}</Label>
				<RadioGroup.Root
					class="mt-2"
					value={provider}
					disabled={isEditMode}
					onValueChange={(value) => {
						provider = value as SecretSourceProvider;
						testResult = null;
					}}
				>
					<div class="grid gap-2">
						{#each providers as option (option.value)}
							<label
								class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40 has-disabled:cursor-not-allowed has-disabled:opacity-70"
							>
								<RadioGroup.Item value={option.value} class="mt-0.5" />
								<ProviderIcon provider={option.value} />
								<div class="grid gap-1 leading-none">
									<span class="text-sm font-medium">{option.label()}</span>
									<span class="text-xs text-muted-foreground">{option.description()}</span>
								</div>
							</label>
						{/each}
					</div>
				</RadioGroup.Root>
			</div>

			<FormInput label={m.common_name()} type="text" placeholder={m.secret_sources_name_placeholder()} bind:input={inputs.name} />

			<FormInput
				label={m.secret_sources_site_url()}
				type="text"
				placeholder="https://app.infisical.com"
				helpText={m.secret_sources_site_url_description()}
				bind:input={inputs.siteUrl}
			/>

			<FormInput label={m.secret_sources_client_id()} type="text" autocomplete="off" bind:input={inputs.clientId} />

			<FormInput
				label={m.secret_sources_client_secret()}
				type="password"
				autocomplete="new-password"
				placeholder={hasStoredSecret ? m.common_keep_placeholder() : ''}
				bind:input={inputs.clientSecret}
			/>

			<FormInput
				label={m.secret_sources_organization_slug()}
				type="text"
				helpText={m.secret_sources_organization_slug_description()}
				bind:input={inputs.organizationSlug}
			/>

			<div class="space-y-3 border-t border-border/50 pt-4">
				<ArcaneButton
					action="test"
					tone="outline"
					type="button"
					customLabel={m.test_connection()}
					loading={testing}
					disabled={testing || isLoading}
					onclick={testConnection}
				/>

				{#if testResult}
					{#if testResult.ok}
						<Alert.Root
							variant="primary-subtle"
							icon={CheckIcon}
							heading={testResult.message}
							description={testResult.canBrowse ? visibleLabel(testResult) : undefined}
						/>
					{:else}
						<Alert.Root
							variant="destructive-subtle"
							icon={AlertIcon}
							heading={m.secret_sources_test_failed()}
							description={testResult.message}
						/>
					{/if}
				{/if}
			</div>
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={isLoading}
			submitAction={isEditMode ? 'save' : 'create'}
			submitForm="secret-source-form"
			submitDisabled={isLoading || testing}
			submitLoading={isLoading}
			submitLabel={isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.secret_source() })}
		/>
	{/snippet}
</ResponsiveDialog.Root>
