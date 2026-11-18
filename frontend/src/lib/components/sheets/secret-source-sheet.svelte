<script lang="ts">
	import { untrack } from 'svelte';
	import { z } from 'zod/v4';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import FormInput from '#lib/components/form/form-input.svelte';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
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
		SecretProvider,
		VaultAuthMethod,
		SecretSource,
		SecretSourceCreateDto,
		SecretSourceTestResult,
		SecretSourceUpdateDto,
		SourceSettings
	} from '#lib/types/secret-source.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type SecretSourceFormPayload =
		| { mode: 'create'; source: SecretSourceCreateDto }
		| { mode: 'edit'; id: string; source: SecretSourceUpdateDto };

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
	const storedSetupClientId = untrack(() => sourceToEdit?.settings.infisical?.setupClientId ?? '');
	const hasStoredSetupSecret = untrack(() => !!sourceToEdit?.hasSetupCredential);

	// The provider is fixed once a source exists; bindings depend on it.
	let provider = $state<SecretProvider>(untrack(() => sourceToEdit?.provider ?? 'infisical'));

	const providers = sortedProviderOptions();
	const storedVault = untrack(() => sourceToEdit?.settings.vault);
	let vaultAuth = $state<VaultAuthMethod>(storedVault?.authMethod ?? 'token');
	let vaultSetupToken = $state(!!storedVault?.setupToken);
	const storedVaultSetupToken = !!storedVault?.setupToken && untrack(() => !!sourceToEdit?.hasSetupCredential);

	// A stored credential is only reused while the address and login method
	// stay the same; the backend refuses otherwise.
	const storedEndpoint = untrack(() => (sourceToEdit ? endpointKey(sourceToEdit.settings) : ''));
	let clearHttpToken = $state(false);

	// Everything except Bitwarden stores a credential; for HTTP and the Proton
	// Pass kit it is an optional bearer token.
	const usesCredential = $derived(provider !== 'bitwarden');
	const optionalToken = $derived(provider === 'http' || provider === 'protonpass');
	const credentialRequired = $derived(provider !== 'bitwarden' && !optionalToken);

	function credentialLabel(current: SecretProvider): string {
		switch (current) {
			case 'vault':
				return vaultAuth === 'approle' ? m.vault_secret_id() : m.vault_token();
			case 'doppler':
				return m.doppler_token();
			case 'onepassword':
				return m.onepassword_connect_token();
			case 'http':
				return m.http_bearer_token();
			case 'protonpass':
				return m.protonpass_kit_token();
			default:
				return m.secret_sources_client_secret();
		}
	}

	const formSchema = untrack(() =>
		z
			.object({
				name: z.string().trim().min(1, m.common_name_required()),
				siteUrl: z.string().trim(),
				clientId: z.string().trim(),
				clientSecret: z.string(),
				organizationSlug: z.string().trim(),
				setupClientId: z.string().trim(),
				setupClientSecret: z.string(),
				serveUrl: z.string().trim(),
				address: z.string().trim(),
				namespace: z.string().trim(),
				roleId: z.string().trim(),
				appRoleMount: z.string().trim(),
				setupToken: z.string(),
				apiUrl: z.string().trim(),
				serverUrl: z.string().trim(),
				baseUrl: z.string().trim()
			})
			.superRefine((data, ctx) => {
				const requireField = (field: 'serveUrl' | 'address' | 'serverUrl' | 'baseUrl' | 'roleId', message: string) => {
					if (!data[field]) ctx.addIssue({ code: 'custom', path: [field], message });
				};
				// Editing keeps the stored credential when the field is left empty,
				// unless the address or login method changed.
				const endpointChanged = !!sourceToEdit && endpointKey(currentSettings()) !== storedEndpoint;
				const requireCredential = () => {
					if (data.clientSecret !== '') return;
					if (!sourceToEdit) {
						ctx.addIssue({ code: 'custom', path: ['clientSecret'], message: m.secret_sources_credential_required() });
					} else if (endpointChanged && hasStoredSecret) {
						ctx.addIssue({ code: 'custom', path: ['clientSecret'], message: m.secret_sources_credential_reenter() });
					}
				};
				switch (provider) {
					case 'bitwarden':
						requireField('serveUrl', m.secret_sources_serve_url_required());
						return;
					case 'vault':
						requireField('address', m.secret_sources_url_required());
						if (vaultAuth === 'approle') requireField('roleId', m.vault_role_id_required());
						requireCredential();
						if (vaultSetupToken && data.setupToken === '' && !storedVaultSetupToken) {
							ctx.addIssue({ code: 'custom', path: ['setupToken'], message: m.secret_sources_credential_required() });
						}
						return;
					case 'doppler':
						requireCredential();
						return;
					case 'onepassword':
						requireField('serverUrl', m.secret_sources_url_required());
						requireCredential();
						return;
					case 'http':
					case 'protonpass':
						requireField('baseUrl', m.secret_sources_url_required());
						if (endpointChanged && hasStoredSecret && !clearHttpToken && data.clientSecret === '') {
							ctx.addIssue({ code: 'custom', path: ['clientSecret'], message: m.secret_sources_credential_reenter() });
						}
						return;
				}
				if (!data.clientId) {
					ctx.addIssue({ code: 'custom', path: ['clientId'], message: m.secret_sources_client_id_required() });
				}
				// Editing keeps the stored secret when the field is left empty.
				if (!sourceToEdit && data.clientSecret === '') {
					ctx.addIssue({ code: 'custom', path: ['clientSecret'], message: m.secret_sources_client_secret_required() });
				}
				if (endpointChanged && hasStoredSecret && data.clientSecret === '') {
					ctx.addIssue({ code: 'custom', path: ['clientSecret'], message: m.secret_sources_credential_reenter() });
				}
				if (data.setupClientId && data.setupClientId === data.clientId) {
					ctx.addIssue({ code: 'custom', path: ['setupClientId'], message: m.secret_sources_setup_same_identity() });
				}
				// A stored setup secret only belongs to the setup identity it was saved with.
				const keepsSetupSecret = hasStoredSetupSecret && data.setupClientId === storedSetupClientId;
				if (data.setupClientId && data.setupClientSecret === '' && !keepsSetupSecret) {
					ctx.addIssue({ code: 'custom', path: ['setupClientSecret'], message: m.secret_sources_client_secret_required() });
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
			organizationSlug: sourceToEdit?.settings.infisical?.organizationSlug ?? '',
			setupClientId: sourceToEdit?.settings.infisical?.setupClientId ?? '',
			setupClientSecret: '',
			serveUrl: sourceToEdit?.settings.bitwarden?.serveUrl ?? '',
			address: sourceToEdit?.settings.vault?.address ?? '',
			namespace: sourceToEdit?.settings.vault?.namespace ?? '',
			roleId: sourceToEdit?.settings.vault?.roleId ?? '',
			appRoleMount: sourceToEdit?.settings.vault?.appRoleMount ?? '',
			setupToken: '',
			apiUrl: sourceToEdit?.settings.doppler?.apiUrl ?? '',
			serverUrl: sourceToEdit?.settings.onepassword?.serverUrl ?? '',
			baseUrl: sourceToEdit?.settings.http?.baseUrl ?? sourceToEdit?.settings.protonpass?.kitUrl ?? ''
		}))
	);
	let inputs = $derived(form.inputs);

	let testing = $state(false);
	let testResult = $state<SecretSourceTestResult | null>(null);

	function currentSettings(): SourceSettings {
		switch (provider) {
			case 'bitwarden':
				return { bitwarden: { serveUrl: inputs.serveUrl.value.trim() } };
			case 'vault':
				return {
					vault: {
						address: inputs.address.value.trim(),
						namespace: inputs.namespace.value.trim() || undefined,
						authMethod: vaultAuth,
						roleId: vaultAuth === 'approle' ? inputs.roleId.value.trim() : undefined,
						appRoleMount: vaultAuth === 'approle' ? inputs.appRoleMount.value.trim() || undefined : undefined,
						setupToken: vaultSetupToken || undefined
					}
				};
			case 'doppler':
				return { doppler: { apiUrl: inputs.apiUrl.value.trim() || undefined } };
			case 'onepassword':
				return { onepassword: { serverUrl: inputs.serverUrl.value.trim() } };
			case 'http':
				return { http: { baseUrl: inputs.baseUrl.value.trim() } };
			case 'protonpass':
				return { protonpass: { kitUrl: inputs.baseUrl.value.trim() } };
		}
		return {
			infisical: {
				siteUrl: inputs.siteUrl.value.trim(),
				clientId: inputs.clientId.value.trim(),
				organizationSlug: inputs.organizationSlug.value.trim(),
				setupClientId: inputs.setupClientId.value.trim() || undefined
			}
		};
	}

	function visibleLabel(result: SecretSourceTestResult): string {
		switch (provider) {
			case 'bitwarden':
				return m.secret_sources_test_visible_items({ count: result.visibleCount });
			case 'vault':
				return m.secret_sources_test_visible_mounts({ count: result.visibleCount });
			case 'onepassword':
			case 'protonpass':
				return m.secret_sources_test_visible_vaults({ count: result.visibleCount });
			default:
				return result.visibleCount === 1
					? m.secret_sources_test_visible_projects_one()
					: m.secret_sources_test_visible_projects({ count: result.visibleCount });
		}
	}

	async function testConnection() {
		testResult = null;
		const missing = (input: { value: string; error: string | null }, message: string) => {
			if (input.value.trim()) return false;
			input.error = message;
			return true;
		};
		const endpointChanged = !!sourceToEdit && endpointKey(currentSettings()) !== storedEndpoint;
		const missingCredential = () => !inputs.clientSecret.value && credentialRequired && (!hasStoredSecret || endpointChanged);
		if (provider === 'bitwarden') {
			if (missing(inputs.serveUrl, m.secret_sources_serve_url_required())) return;
		} else if (provider !== 'infisical') {
			const urlInput =
				provider === 'vault'
					? inputs.address
					: provider === 'onepassword'
						? inputs.serverUrl
						: optionalToken
							? inputs.baseUrl
							: null;
			if (urlInput && missing(urlInput, m.secret_sources_url_required())) return;
			if (missingCredential()) {
				inputs.clientSecret.error = endpointChanged
					? m.secret_sources_credential_reenter()
					: m.secret_sources_credential_required();
				return;
			}
		} else {
			if (!inputs.clientId.value.trim()) {
				inputs.clientId.error = m.secret_sources_client_id_required();
				return;
			}
			if (!inputs.clientSecret.value && !hasStoredSecret) {
				inputs.clientSecret.error = m.secret_sources_client_secret_required();
				return;
			}
		}

		testing = true;
		const result = await tryCatch(
			secretSourceService.test({
				sourceId: sourceToEdit?.id,
				provider,
				settings: currentSettings(),
				credential: usesCredential ? inputs.clientSecret.value || undefined : undefined
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
		const credential = usesCredential && data.clientSecret ? data.clientSecret : undefined;
		let setupCredential: string | undefined;
		if (provider === 'infisical' && data.setupClientId && data.setupClientSecret) setupCredential = data.setupClientSecret;
		if (provider === 'vault' && vaultSetupToken && data.setupToken) setupCredential = data.setupToken;

		if (isEditMode && sourceToEdit) {
			const clearCredential = optionalToken && clearHttpToken && !credential ? true : undefined;
			onSubmit({
				mode: 'edit',
				id: sourceToEdit.id,
				source: { name: data.name, settings, credential, setupCredential, clearCredential }
			});
			return;
		}
		onSubmit({ mode: 'create', source: { name: data.name, provider, settings, credential, setupCredential } });
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
						provider = value as SecretProvider;
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

			{#if provider === 'bitwarden'}
				<FormInput
					label={m.secret_sources_serve_url()}
					type="text"
					placeholder="http://bw-serve:8087"
					helpText={m.secret_sources_serve_url_description()}
					bind:input={inputs.serveUrl}
				/>
				<Alert.Root variant="info" icon={AlertIcon} description={m.secret_sources_bitwarden_hardening()} />
			{:else if provider === 'vault'}
				<FormInput
					label={m.vault_address()}
					type="text"
					placeholder="http://openbao:8200"
					helpText={m.vault_address_description()}
					bind:input={inputs.address}
				/>
				<div class="space-y-2">
					<Label class="mb-0">{m.vault_auth_method()}</Label>
					<RadioGroup.Root
						class="mt-2"
						value={vaultAuth}
						onValueChange={(value) => {
							vaultAuth = value as VaultAuthMethod;
							testResult = null;
						}}
					>
						<div class="grid grid-cols-2 gap-2">
							{#each [{ value: 'token', label: m.vault_auth_token() }, { value: 'approle', label: m.vault_auth_approle() }] as option (option.value)}
								<label class="flex cursor-pointer items-center gap-2 rounded-md border border-border/50 p-2.5 hover:bg-accent/40">
									<RadioGroup.Item value={option.value} />
									<span class="text-sm">{option.label}</span>
								</label>
							{/each}
						</div>
					</RadioGroup.Root>
				</div>
				{#if vaultAuth === 'approle'}
					<FormInput label={m.vault_role_id()} type="text" autocomplete="off" bind:input={inputs.roleId} />
				{/if}
				<FormInput
					label={credentialLabel(provider)}
					type="password"
					autocomplete="new-password"
					placeholder={hasStoredSecret ? m.common_keep_placeholder() : ''}
					helpText={m.vault_token_description()}
					bind:input={inputs.clientSecret}
				/>
				{#if vaultAuth === 'approle'}
					<FormInput label={m.vault_approle_mount()} type="text" placeholder="approle" bind:input={inputs.appRoleMount} />
				{/if}
				<FormInput
					label={m.vault_namespace()}
					type="text"
					helpText={m.vault_namespace_description()}
					bind:input={inputs.namespace}
				/>
				<div class="grid gap-3 rounded-lg border border-border/50 p-3">
					<SwitchWithLabel
						id="vault-setup-token"
						label={m.vault_setup_token()}
						description={m.vault_setup_token_description()}
						bind:checked={vaultSetupToken}
					/>
					{#if vaultSetupToken}
						<FormInput
							label={m.vault_setup_token_value()}
							type="password"
							autocomplete="new-password"
							placeholder={storedVaultSetupToken ? m.common_keep_placeholder() : ''}
							bind:input={inputs.setupToken}
						/>
					{/if}
				</div>
			{:else if provider === 'doppler'}
				<FormInput
					label={credentialLabel(provider)}
					type="password"
					autocomplete="new-password"
					placeholder={hasStoredSecret ? m.common_keep_placeholder() : 'dp.st.prd.…'}
					helpText={m.doppler_token_description()}
					bind:input={inputs.clientSecret}
				/>
				<FormInput
					label={m.doppler_api_url()}
					type="text"
					placeholder="https://api.doppler.com"
					helpText={m.doppler_api_url_description()}
					bind:input={inputs.apiUrl}
				/>
			{:else if provider === 'onepassword'}
				<FormInput
					label={m.onepassword_server_url()}
					type="text"
					placeholder="http://op-connect-api:8080"
					helpText={m.onepassword_server_url_description()}
					bind:input={inputs.serverUrl}
				/>
				<FormInput
					label={credentialLabel(provider)}
					type="password"
					autocomplete="new-password"
					placeholder={hasStoredSecret ? m.common_keep_placeholder() : ''}
					bind:input={inputs.clientSecret}
				/>
			{:else if optionalToken}
				<FormInput
					label={provider === 'protonpass' ? m.protonpass_kit_url() : m.http_base_url()}
					type="text"
					placeholder={provider === 'protonpass' ? 'http://pass-kit:8080' : 'http://sops-kit:8080/secrets'}
					helpText={provider === 'protonpass' ? m.protonpass_kit_url_description() : m.http_base_url_description()}
					bind:input={inputs.baseUrl}
				/>
				<FormInput
					label={credentialLabel(provider)}
					type="password"
					autocomplete="new-password"
					placeholder={hasStoredSecret ? m.common_keep_placeholder() : m.http_token_optional()}
					helpText={m.http_bearer_token_description()}
					bind:input={inputs.clientSecret}
				/>
				{#if isEditMode && hasStoredSecret}
					<SwitchWithLabel
						id="http-clear-token"
						label={m.http_remove_token()}
						description={m.http_remove_token_description()}
						bind:checked={clearHttpToken}
					/>
				{/if}
				{#if provider === 'protonpass'}
					<Alert.Root variant="info" icon={AlertIcon} description={m.protonpass_kit_hint()} />
				{:else}
					<Alert.Root variant="info" icon={AlertIcon} description={m.http_contract_hint({ example: '{"DB_PASSWORD": "…"}' })} />
				{/if}
			{:else}
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

				<div class="grid gap-3 rounded-lg border border-border/50 p-3">
					<div class="space-y-1">
						<p class="text-sm font-medium">{m.secret_sources_setup_identity()}</p>
						<p class="text-xs text-muted-foreground">{m.secret_sources_setup_identity_description()}</p>
					</div>
					<FormInput
						label={m.secret_sources_setup_client_id()}
						type="text"
						autocomplete="off"
						bind:input={inputs.setupClientId}
					/>
					<FormInput
						label={m.secret_sources_setup_client_secret()}
						type="password"
						autocomplete="new-password"
						placeholder={hasStoredSetupSecret ? m.common_keep_placeholder() : ''}
						helpText={m.secret_sources_setup_clear_hint()}
						bind:input={inputs.setupClientSecret}
					/>
				</div>
			{/if}

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
