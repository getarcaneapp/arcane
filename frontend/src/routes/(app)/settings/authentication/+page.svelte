<script lang="ts">
	import type { PageProps } from './$types';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { z } from 'zod/v4';
	import { untrack } from 'svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { toast } from 'svelte-sonner';
	import type { Settings } from '#lib/types/settings.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { LockIcon, InfoIcon, ArrowDownIcon } from '#lib/icons/index.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { createSettingsForm } from '#lib/utils/settings-form.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { cn } from '#lib/utils.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import OidcMappingTable from './components/oidc-mapping-table.svelte';
	import FederatedCredentialsTab from './components/federated-credentials-tab.svelte';
	import OidcMappingFormSheet from '#lib/components/sheets/oidc-mapping-form-sheet.svelte';
	import type { OidcRoleMapping, CreateOidcRoleMapping, UpdateOidcRoleMapping } from '#lib/types/auth.js';
	import { oidcMappingService } from '#lib/services/oidc-mapping-service.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { mergeProps } from 'bits-ui';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';

	let { data }: PageProps = $props();
	type AuthenticationTab = 'settings' | 'federated';

	const authenticationTabItems = $derived.by(
		() =>
			[
				{
					value: 'settings',
					label: m.authentication()
				},
				{
					value: 'federated',
					label: m.federated_credential_page_title()
				}
			] satisfies TabItem[]
	);
	const urlTab = useUrlTab<AuthenticationTab>({
		validTabs: () => authenticationTabItems.map((tab) => tab.value as AuthenticationTab),
		defaultTab: () => 'settings'
	});
	const activeTab = $derived(urlTab.value);

	// OIDC role mappings — co-located with the OIDC settings so admins
	// configure the groups claim and the mappings that read it in one place.
	let oidcMappings: OidcRoleMapping[] = $derived(data.oidcMappings ?? []);
	let mappingSheetOpen = $state(false);
	let editingMapping: OidcRoleMapping | null = $state(null);
	let mappingSaving = $state(false);

	async function refreshMappings() {
		oidcMappings = await oidcMappingService.list();
	}

	function openCreateMapping() {
		editingMapping = null;
		mappingSheetOpen = true;
	}

	function openEditMapping(mapping: OidcRoleMapping) {
		editingMapping = mapping;
		mappingSheetOpen = true;
	}

	async function submitMapping(formData: { claimValue: string; roleId: string; environmentId?: string }) {
		mappingSaving = true;
		const payload: CreateOidcRoleMapping | UpdateOidcRoleMapping = formData;

		if (editingMapping) {
			const id = editingMapping.id;
			await handleApiResultWithCallbacks({
				result: await tryCatch(oidcMappingService.update(id, payload)),
				message: m.common_update_failed({ resource: m.resource_oidc_mapping() }),
				setLoadingState: (v) => (mappingSaving = v),
				onSuccess: async () => {
					toast.success(m.common_update_success({ resource: m.resource_oidc_mapping_cap() }));
					await refreshMappings();
					mappingSheetOpen = false;
					editingMapping = null;
				}
			});
		} else {
			await handleApiResultWithCallbacks({
				result: await tryCatch(oidcMappingService.create(payload)),
				message: m.common_create_failed({ resource: m.resource_oidc_mapping() }),
				setLoadingState: (v) => (mappingSaving = v),
				onSuccess: async () => {
					toast.success(m.common_create_success({ resource: m.resource_oidc_mapping_cap() }));
					await refreshMappings();
					mappingSheetOpen = false;
				}
			});
		}
	}
	const currentSettings = $derived<Settings>(settingsStore.current || data.settings!);
	const isReadOnly = $derived.by(() => settingsStore.current?.uiConfigDisabled);
	const isAutoLoginEnabled = $derived(settingsStore.autoLoginEnabled.current);

	const formSchema = z
		.object({
			authLocalEnabled: z.boolean(),
			authSessionTimeout: z.coerce
				.number()
				.int(m.security_session_timeout_integer())
				.min(15, m.security_session_timeout_min())
				.max(525600, m.security_session_timeout_max()),
			authPasswordPolicy: z.enum(['basic', 'standard', 'strong']),
			oidcEnabled: z.boolean(),
			oidcMergeAccounts: z.boolean(),
			oidcSkipTlsVerify: z.boolean(),
			oidcAutoRedirectToProvider: z.boolean(),
			oidcClientId: z.string(),
			oidcClientSecret: z.string(),
			oidcClearClientSecret: z.boolean(),
			oidcIssuerUrl: z.string(),
			oidcScopes: z.string(),
			oidcGroupsClaim: z.string(),
			oidcProviderName: z.string(),
			oidcProviderLogoUrl: z.string()
		})
		.superRefine((formData, ctx) => {
			const oidcEnabledForAuthValidation = data.oidcStatus.envForced ? currentSettings.oidcEnabled : formData.oidcEnabled;
			if (!oidcEnabledForAuthValidation && !formData.authLocalEnabled) {
				ctx.addIssue({
					code: 'custom',
					message: m.security_enable_one_provider(),
					path: ['authLocalEnabled']
				});
			}
			if (formData.oidcEnabled && !data.oidcStatus.envForced) {
				for (const field of ['oidcClientId', 'oidcIssuerUrl'] as const) {
					if (!formData[field].trim()) {
						ctx.addIssue({ code: 'custom', message: m.security_oidc_required_fields(), path: [field] });
					}
				}
				if ((!currentSettings.oidcClientSecret || formData.oidcClearClientSecret) && !formData.oidcClientSecret.trim()) {
					ctx.addIssue({ code: 'custom', message: m.security_oidc_required_fields(), path: ['oidcClientSecret'] });
				}
			}
		});

	let showMergeAccountsAlert = $state(false);

	function readFormValues() {
		const source = settingsStore.current || data.settings!;
		return {
			authLocalEnabled: source.authLocalEnabled,
			authSessionTimeout: source.authSessionTimeout,
			authPasswordPolicy: source.authPasswordPolicy,
			oidcEnabled: source.oidcEnabled,
			oidcMergeAccounts: source.oidcMergeAccounts,
			oidcSkipTlsVerify: source.oidcSkipTlsVerify,
			oidcAutoRedirectToProvider: source.oidcAutoRedirectToProvider,
			oidcClientId: source.oidcClientId,
			oidcClientSecret: '',
			oidcClearClientSecret: false,
			oidcIssuerUrl: source.oidcIssuerUrl,
			oidcScopes: source.oidcScopes,
			oidcGroupsClaim: source.oidcGroupsClaim,
			oidcProviderName: source.oidcProviderName,
			oidcProviderLogoUrl: source.oidcProviderLogoUrl
		};
	}

	const { formInputs } = untrack(() =>
		createSettingsForm({
			schema: formSchema,
			currentSettings: readFormValues(),
			getCurrentSettings: readFormValues,
			onSave: async ({ oidcClientSecret, oidcClearClientSecret, ...rest }) => {
				const payload: Partial<Settings> = rest;
				if (oidcClearClientSecret) payload.oidcClientSecret = '';
				else if (oidcClientSecret) payload.oidcClientSecret = oidcClientSecret;
				await settingsService.updateSettings(payload);
			},
			onSuccess: () => {
				formInputs.oidcClientSecret.value = '';
				formInputs.oidcClearClientSecret.value = false;
			},
			successMessage: m.security_settings_saved(),
			errorMessage: m.security_settings_save_failed()
		})
	);

	const redirectUri = $derived(`${globalThis?.location?.origin ?? ''}/auth/oidc/callback`);
	const isOidcEnvForced = $derived(data.oidcStatus.envForced);
	const isOidcForcedEnabled = $derived(isOidcEnvForced && currentSettings.oidcEnabled);
	const isOidcForcedDisabled = $derived(isOidcEnvForced && !currentSettings.oidcEnabled);
	const isOidcEnabledForAuthValidation = $derived.by(() =>
		isOidcEnvForced ? currentSettings.oidcEnabled : formInputs.oidcEnabled.value
	);
	const showOidcDetails = $derived(formInputs.oidcEnabled.value || isOidcForcedEnabled);
	const hasStoredClientSecret = $derived(!!currentSettings.oidcClientSecret);

	let oidcConfigOpen = $derived(
		!!(formInputs.oidcClientId.error || formInputs.oidcClientSecret.error || formInputs.oidcIssuerUrl.error)
	);

	function handleLocalSwitchChange(checked: boolean) {
		if (!checked && !isOidcEnabledForAuthValidation) {
			formInputs.authLocalEnabled.value = true;
			toast.error(m.security_enable_one_provider_error());
			return;
		}
		formInputs.authLocalEnabled.value = checked;
	}

	function handleOidcEnabledChange(checked: boolean) {
		if (!checked && !formInputs.authLocalEnabled.value && !isOidcEnvForced) {
			formInputs.authLocalEnabled.value = true;
			toast.info(m.security_local_enabled_info());
		}
		formInputs.oidcEnabled.value = checked;
	}

	function handleMergeAccountsChange(checked: boolean) {
		if (checked && !currentSettings.oidcMergeAccounts) {
			showMergeAccountsAlert = true;
		} else {
			formInputs.oidcMergeAccounts.value = checked;
		}
	}

	function confirmMergeAccounts() {
		formInputs.oidcMergeAccounts.value = true;
		showMergeAccountsAlert = false;
	}

	function cancelMergeAccounts() {
		formInputs.oidcMergeAccounts.value = false;
		showMergeAccountsAlert = false;
	}
</script>

{#snippet passwordPolicyOption(value: 'basic' | 'standard' | 'strong', label: string, tooltip: string)}
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger>
			{#snippet child({ props })}
				{@const triggerProps = mergeProps(props, {
					onclick: () => (formInputs.authPasswordPolicy.value = value),
					class: 'h-12 w-full text-xs sm:text-sm'
				})}
				<ArcaneButton
					{...triggerProps}
					action="base"
					tone={formInputs.authPasswordPolicy.value === value ? 'outline-primary' : 'outline'}
					customLabel={label}
				/>
			{/snippet}
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content side="top">{tooltip}</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{/snippet}

<SettingsPageLayout
	title={m.authentication()}
	description={m.authentication_description()}
	icon={LockIcon}
	pageType={activeTab === 'settings' ? 'form' : 'management'}
	showReadOnlyTag={activeTab === 'settings' && isReadOnly}
>
	{#snippet mainContent()}
		<Tabs.Root value={activeTab}>
			<TabBar items={authenticationTabItems} value={activeTab} onValueChange={urlTab.select} />

			<Tabs.Content value="settings" class="mt-6">
				<fieldset disabled={isReadOnly} class="relative space-y-8">
					<SettingsSection title={m.authentication()}>
						{#if isAutoLoginEnabled}
							<div class="px-5 py-4">
								<Alert.Root variant="warning-subtle">
									<InfoIcon class="h-4 w-4 text-warning" />
									<Alert.Title>{m.security_auto_login_enabled_title()}</Alert.Title>
									<Alert.Description>{m.security_auto_login_enabled_description()}</Alert.Description>
								</Alert.Root>
							</div>
						{:else}
							<SettingsRow
								for="localAuthSwitch"
								label={m.security_local_auth_label()}
								description={m.security_local_auth_description()}
								layout="switch"
							>
								<Switch
									id="localAuthSwitch"
									bind:checked={formInputs.authLocalEnabled.value}
									onCheckedChange={handleLocalSwitchChange}
								/>
							</SettingsRow>

							<SettingsRow
								for="oidcEnabledSwitch"
								label={m.security_oidc_auth_label()}
								description={m.security_oidc_auth_description()}
								layout="switch"
							>
								{#snippet labelExtra()}
									{#if isOidcEnvForced}
										<div class="mt-2">
											<ArcaneTooltip.Root>
												<ArcaneTooltip.Trigger>
													<Badge variant="amber">
														{#if isOidcForcedDisabled}
															{m.security_server_disabled_via_server()}
														{:else}
															{m.security_server_configured()}
														{/if}
													</Badge>
												</ArcaneTooltip.Trigger>
												<ArcaneTooltip.Content side="top">
													{#if isOidcForcedDisabled}
														{m.security_oidc_forced_disabled_tooltip()}
													{:else}
														{m.security_oidc_forced_managed_tooltip()}
													{/if}
												</ArcaneTooltip.Content>
											</ArcaneTooltip.Root>
										</div>
									{/if}
								{/snippet}
								<div class="flex flex-col items-end gap-2">
									<Switch
										id="oidcEnabledSwitch"
										disabled={isOidcEnvForced}
										bind:checked={formInputs.oidcEnabled.value}
										onCheckedChange={handleOidcEnabledChange}
									/>
									{#if showOidcDetails}
										<button
											type="button"
											class="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground"
											aria-expanded={oidcConfigOpen}
											onclick={() => (oidcConfigOpen = !oidcConfigOpen)}
										>
											<span>{oidcConfigOpen ? m.common_hide() : m.common_show()} {m.common_configuration()}</span>
											<ArrowDownIcon class={cn('size-3.5 transition-transform', oidcConfigOpen && 'rotate-180')} />
										</button>
									{/if}
								</div>
							</SettingsRow>

							{#if showOidcDetails && oidcConfigOpen}
								<SettingsRow for="oidcClientId" label={m.oidc_client_id_label()} error={formInputs.oidcClientId.error}>
									<Input
										id="oidcClientId"
										placeholder={m.oidc_client_id_placeholder()}
										disabled={isOidcEnvForced}
										bind:value={formInputs.oidcClientId.value}
										aria-invalid={!!formInputs.oidcClientId.error}
									/>
								</SettingsRow>
								<SettingsRow
									for="oidcClientSecret"
									label={m.oidc_client_secret_label()}
									helpText={m.security_oidc_client_secret_help()}
									error={formInputs.oidcClientSecret.error}
								>
									<Input
										id="oidcClientSecret"
										type="password"
										placeholder={m.oidc_client_secret_placeholder()}
										disabled={isOidcEnvForced || formInputs.oidcClearClientSecret.value}
										bind:value={formInputs.oidcClientSecret.value}
										aria-invalid={!!formInputs.oidcClientSecret.error}
									/>
								</SettingsRow>
								{#if hasStoredClientSecret && !isOidcEnvForced}
									<SettingsRow for="clearOidcClientSecret" label={m.security_oidc_clear_client_secret()} layout="switch">
										<Switch id="clearOidcClientSecret" bind:checked={formInputs.oidcClearClientSecret.value} />
									</SettingsRow>
								{/if}
								<SettingsRow
									for="oidcIssuerUrl"
									label={m.oidc_issuer_url_label()}
									description={m.oidc_issuer_url_description()}
									error={formInputs.oidcIssuerUrl.error}
								>
									<Input
										id="oidcIssuerUrl"
										placeholder={m.oidc_issuer_url_placeholder()}
										disabled={isOidcEnvForced}
										bind:value={formInputs.oidcIssuerUrl.value}
										aria-invalid={!!formInputs.oidcIssuerUrl.error}
									/>
								</SettingsRow>
								<SettingsRow
									for="oidcProviderName"
									label={m.oidc_provider_name_label()}
									description={m.oidc_provider_name_description()}
									error={formInputs.oidcProviderName.error}
								>
									<Input
										id="oidcProviderName"
										placeholder={m.oidc_provider_name_placeholder()}
										disabled={isOidcEnvForced}
										bind:value={formInputs.oidcProviderName.value}
										aria-invalid={!!formInputs.oidcProviderName.error}
									/>
								</SettingsRow>
								<SettingsRow
									for="oidcProviderLogoUrl"
									label={m.oidc_provider_logo_url_label()}
									description={m.oidc_provider_logo_url_description()}
									error={formInputs.oidcProviderLogoUrl.error}
								>
									<Input
										id="oidcProviderLogoUrl"
										placeholder={m.oidc_provider_logo_url_placeholder()}
										disabled={isOidcEnvForced}
										bind:value={formInputs.oidcProviderLogoUrl.value}
										aria-invalid={!!formInputs.oidcProviderLogoUrl.error}
									/>
								</SettingsRow>
								<SettingsRow for="oidcScopes" label={m.oidc_scopes_label()} error={formInputs.oidcScopes.error}>
									<Input
										id="oidcScopes"
										placeholder={m.oidc_scopes_placeholder()}
										disabled={isOidcEnvForced}
										bind:value={formInputs.oidcScopes.value}
										aria-invalid={!!formInputs.oidcScopes.error}
									/>
								</SettingsRow>
								<SettingsRow
									for="oidcGroupsClaim"
									label={m.oidc_groups_claim_label()}
									helpText={m.oidc_groups_claim_help()}
									error={formInputs.oidcGroupsClaim.error}
								>
									<Input
										id="oidcGroupsClaim"
										placeholder={m.oidc_groups_claim_placeholder()}
										disabled={isOidcEnvForced}
										bind:value={formInputs.oidcGroupsClaim.value}
										aria-invalid={!!formInputs.oidcGroupsClaim.error}
									/>
								</SettingsRow>
								<SettingsRow
									for="oidcMergeAccountsSwitch"
									label={m.security_oidc_merge_accounts_label()}
									description={m.security_oidc_merge_accounts_description()}
									layout="switch"
								>
									<Switch
										id="oidcMergeAccountsSwitch"
										disabled={isOidcEnvForced}
										bind:checked={formInputs.oidcMergeAccounts.value}
										onCheckedChange={handleMergeAccountsChange}
									/>
								</SettingsRow>
								<SettingsRow
									for="oidcSkipTlsVerifySwitch"
									label={m.oidc_skip_tls_verify_label()}
									description={m.oidc_skip_tls_verify_description()}
									layout="switch"
								>
									<Switch
										id="oidcSkipTlsVerifySwitch"
										disabled={isOidcEnvForced}
										bind:checked={formInputs.oidcSkipTlsVerify.value}
									/>
								</SettingsRow>
								<SettingsRow
									for="oidcAutoRedirectSwitch"
									label={m.oidc_auto_redirect_label()}
									description={m.oidc_auto_redirect_description()}
									layout="switch"
								>
									<Switch
										id="oidcAutoRedirectSwitch"
										disabled={isOidcEnvForced}
										bind:checked={formInputs.oidcAutoRedirectToProvider.value}
									/>
								</SettingsRow>
								<SettingsRow label={m.oidc_redirect_uri_title()} description={m.oidc_redirect_uri_description()} layout="wide">
									<div class="flex items-center gap-2">
										<code class="flex-1 rounded-md bg-muted/60 p-2 font-mono text-xs break-all">{redirectUri}</code>
										<CopyButton text={redirectUri} size="sm" variant="outline" class="shrink-0" title={m.common_copy()} />
									</div>
								</SettingsRow>
							{/if}
						{/if}
					</SettingsSection>

					{#if !isAutoLoginEnabled}
						<IfPermitted adminOnly>
							<SettingsSection
								title={m.oidc_role_mappings_title()}
								description={m.oidc_role_mappings_description()}
								variant="plain"
							>
								{#snippet actions()}
									<ArcaneButton
										action="create"
										tone="outline"
										size="sm"
										onclick={openCreateMapping}
										customLabel={m.common_create_button({ resource: m.resource_oidc_mapping_cap() })}
									/>
								{/snippet}
								{#if oidcMappings.length > 0}
									<OidcMappingTable
										mappings={oidcMappings}
										roles={data.roles}
										environments={data.environments}
										onRefresh={refreshMappings}
										onEdit={openEditMapping}
									/>
								{:else}
									<div class="rounded-xl border border-dashed border-border/60 p-6 text-center">
										<p class="text-sm text-muted-foreground">{m.oidc_mappings_empty_body()}</p>
									</div>
								{/if}
							</SettingsSection>
						</IfPermitted>
					{/if}

					<SettingsSection title={m.security_session_heading()}>
						<SettingsRow
							for="authSessionTimeout"
							label={m.security_session_timeout_label()}
							description={m.security_session_timeout_description()}
							error={formInputs.authSessionTimeout.error}
						>
							<Input
								id="authSessionTimeout"
								type="number"
								bind:value={formInputs.authSessionTimeout.value}
								aria-invalid={!!formInputs.authSessionTimeout.error}
							/>
						</SettingsRow>
					</SettingsSection>

					<SettingsSection title={m.security_password_policy_label()} description={m.security_password_policy_description()}>
						<div
							class="grid grid-cols-1 gap-2 px-5 py-4 sm:grid-cols-3 sm:gap-3"
							role="group"
							aria-label={m.security_password_policy_label()}
						>
							{@render passwordPolicyOption('basic', m.common_basic(), m.security_password_policy_basic_tooltip())}
							{@render passwordPolicyOption(
								'standard',
								m.security_password_policy_standard(),
								m.security_password_policy_standard_tooltip()
							)}
							{@render passwordPolicyOption(
								'strong',
								m.security_password_policy_strong(),
								m.security_password_policy_strong_tooltip()
							)}
						</div>
					</SettingsSection>
				</fieldset>
			</Tabs.Content>

			<Tabs.Content value="federated" class="mt-6">
				<FederatedCredentialsTab
					initialFederatedCredentials={data.federatedCredentials}
					initialRequestOptions={data.federatedCredentialRequestOptions}
					roles={data.roles}
					environments={data.environments}
				/>
			</Tabs.Content>
		</Tabs.Root>
	{/snippet}
	{#snippet additionalContent()}
		<AlertDialog.Root bind:open={showMergeAccountsAlert}>
			<AlertDialog.Content>
				<AlertDialog.Header>
					<AlertDialog.Title>{m.security_oidc_merge_accounts_alert_title()}</AlertDialog.Title>
					<AlertDialog.Description>
						{m.security_oidc_merge_accounts_alert_description()}
					</AlertDialog.Description>
				</AlertDialog.Header>
				<AlertDialog.Footer>
					<AlertDialog.Cancel onclick={cancelMergeAccounts}>{m.common_cancel()}</AlertDialog.Cancel>
					<AlertDialog.Action onclick={confirmMergeAccounts}>{m.common_confirm()}</AlertDialog.Action>
				</AlertDialog.Footer>
			</AlertDialog.Content>
		</AlertDialog.Root>

		<OidcMappingFormSheet
			bind:open={mappingSheetOpen}
			mappingToEdit={editingMapping}
			isLoading={mappingSaving}
			roles={data.roles}
			environments={data.environments}
			onSubmit={submitMapping}
		/>
	{/snippet}
</SettingsPageLayout>
