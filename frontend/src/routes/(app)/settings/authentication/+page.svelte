<script lang="ts">
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { z } from 'zod/v4';
	import { getContext } from 'svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { toast } from 'svelte-sonner';
	import type { Settings } from '$lib/types/settings';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import { m } from '$lib/paraglide/messages';
	import { LockIcon, InfoIcon, ArrowDownIcon } from '$lib/icons';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsPageLayout } from '$lib/layouts';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { createSettingsForm } from '$lib/utils/settings';
	import * as Alert from '$lib/components/ui/alert';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import SettingsRow from '$lib/components/settings/settings-row.svelte';
	import { cn } from '$lib/utils';
	import OidcMappingTable from './oidc-mapping-table.svelte';
	import OidcMappingFormSheet from '$lib/components/sheets/oidc-mapping-form-sheet.svelte';
	import type { OidcRoleMapping, CreateOidcRoleMapping, UpdateOidcRoleMapping } from '$lib/types/auth';
	import { oidcMappingService } from '$lib/services/oidc-mapping-service';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import { untrack } from 'svelte';
	import IfPermitted from '$lib/components/if-permitted.svelte';

	let { data }: PageProps = $props();

	// OIDC role mappings — co-located with the OIDC settings so admins
	// configure the groups claim and the mappings that read it in one place.
	let oidcMappings = $state<OidcRoleMapping[]>(untrack(() => data.oidcMappings ?? []));
	let mappingSheetOpen = $state(false);
	let editingMapping = $state<OidcRoleMapping | null>(null);
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
			handleApiResultWithCallbacks({
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
			handleApiResultWithCallbacks({
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
	const currentSettings = $derived<Settings>($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);
	const isAutoLoginEnabled = $derived(settingsStore.autoLoginEnabled.isEnabled());

	const formSchema = z
		.object({
			authLocalEnabled: z.boolean(),
			authSessionTimeout: z.coerce
				.number()
				.int(m.security_session_timeout_integer())
				.min(15, m.security_session_timeout_min())
				.max(1440, m.security_session_timeout_max()),
			authPasswordPolicy: z.enum(['basic', 'standard', 'strong']),
			oidcEnabled: z.boolean(),
			oidcMergeAccounts: z.boolean(),
			oidcSkipTlsVerify: z.boolean(),
			oidcAutoRedirectToProvider: z.boolean(),
			oidcClientId: z.string(),
			oidcClientSecret: z.string(),
			oidcIssuerUrl: z.string(),
			oidcScopes: z.string(),
			oidcGroupsClaim: z.string(),
			oidcProviderName: z.string(),
			oidcProviderLogoUrl: z.string()
		})
		.superRefine((formData, ctx) => {
			const oidcEnabledForAuthValidation = data.oidcStatus.envForced ? currentSettings.oidcEnabled : formData.oidcEnabled;
			if (oidcEnabledForAuthValidation) return;
			if (!formData.authLocalEnabled) {
				ctx.addIssue({
					code: 'custom',
					message: m.security_enable_one_provider(),
					path: ['authLocalEnabled']
				});
			}
		});

	let showMergeAccountsAlert = $state(false);
	let oidcConfigOpen = $state(false);

	const formDefaults = $derived({
		authLocalEnabled: currentSettings.authLocalEnabled,
		authSessionTimeout: currentSettings.authSessionTimeout,
		authPasswordPolicy: currentSettings.authPasswordPolicy,
		oidcEnabled: currentSettings.oidcEnabled,
		oidcMergeAccounts: currentSettings.oidcMergeAccounts,
		oidcSkipTlsVerify: currentSettings.oidcSkipTlsVerify,
		oidcAutoRedirectToProvider: currentSettings.oidcAutoRedirectToProvider,
		oidcClientId: currentSettings.oidcClientId,
		oidcClientSecret: '',
		oidcIssuerUrl: currentSettings.oidcIssuerUrl,
		oidcScopes: currentSettings.oidcScopes,
		oidcGroupsClaim: currentSettings.oidcGroupsClaim,
		oidcProviderName: currentSettings.oidcProviderName,
		oidcProviderLogoUrl: currentSettings.oidcProviderLogoUrl
	});

	let { formInputs, form, settingsForm } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings: formDefaults,
			getCurrentSettings: () => ({
				authLocalEnabled: ($settingsStore || data.settings!).authLocalEnabled,
				authSessionTimeout: ($settingsStore || data.settings!).authSessionTimeout,
				authPasswordPolicy: ($settingsStore || data.settings!).authPasswordPolicy,
				oidcEnabled: ($settingsStore || data.settings!).oidcEnabled,
				oidcMergeAccounts: ($settingsStore || data.settings!).oidcMergeAccounts,
				oidcSkipTlsVerify: ($settingsStore || data.settings!).oidcSkipTlsVerify,
				oidcAutoRedirectToProvider: ($settingsStore || data.settings!).oidcAutoRedirectToProvider,
				oidcClientId: ($settingsStore || data.settings!).oidcClientId,
				oidcClientSecret: '',
				oidcIssuerUrl: ($settingsStore || data.settings!).oidcIssuerUrl,
				oidcScopes: ($settingsStore || data.settings!).oidcScopes,
				oidcGroupsClaim: ($settingsStore || data.settings!).oidcGroupsClaim,
				oidcProviderName: ($settingsStore || data.settings!).oidcProviderName,
				oidcProviderLogoUrl: ($settingsStore || data.settings!).oidcProviderLogoUrl
			}),
			successMessage: m.security_settings_saved()
		})
	);

	const hasAuthenticationChanges = $derived(
		$formInputs.authLocalEnabled.value !== currentSettings.authLocalEnabled ||
			$formInputs.authSessionTimeout.value !== currentSettings.authSessionTimeout ||
			$formInputs.authPasswordPolicy.value !== currentSettings.authPasswordPolicy ||
			$formInputs.oidcEnabled.value !== currentSettings.oidcEnabled ||
			$formInputs.oidcMergeAccounts.value !== currentSettings.oidcMergeAccounts ||
			$formInputs.oidcSkipTlsVerify.value !== currentSettings.oidcSkipTlsVerify ||
			$formInputs.oidcAutoRedirectToProvider.value !== currentSettings.oidcAutoRedirectToProvider ||
			$formInputs.oidcClientId.value !== currentSettings.oidcClientId ||
			$formInputs.oidcIssuerUrl.value !== currentSettings.oidcIssuerUrl ||
			$formInputs.oidcScopes.value !== currentSettings.oidcScopes ||
			$formInputs.oidcGroupsClaim.value !== currentSettings.oidcGroupsClaim ||
			$formInputs.oidcProviderName.value !== currentSettings.oidcProviderName ||
			$formInputs.oidcProviderLogoUrl.value !== currentSettings.oidcProviderLogoUrl ||
			$formInputs.oidcClientSecret.value !== ''
	);

	const redirectUri = $derived(`${globalThis?.location?.origin ?? ''}/auth/oidc/callback`);
	const isOidcEnvForced = $derived(data.oidcStatus.envForced);
	const isOidcForcedEnabled = $derived(isOidcEnvForced && currentSettings.oidcEnabled);
	const isOidcForcedDisabled = $derived(isOidcEnvForced && !currentSettings.oidcEnabled);
	const isOidcEnabledForAuthValidation = $derived.by(() =>
		isOidcEnvForced ? currentSettings.oidcEnabled : $formInputs.oidcEnabled.value
	);
	const showOidcDetails = $derived($formInputs.oidcEnabled.value || isOidcForcedEnabled);

	async function customSubmit() {
		const formData = form.validate();
		if (!formData) {
			toast.error(m.security_form_validation_error());
			return;
		}

		if (formData.oidcEnabled && !isOidcEnvForced) {
			if (!formData.oidcClientId || !formData.oidcIssuerUrl) {
				toast.error(m.security_oidc_required_fields());
				return;
			}
		}

		settingsForm.setLoading(true);

		try {
			await settingsForm.updateSettings({
				authLocalEnabled: formData.authLocalEnabled,
				authSessionTimeout: formData.authSessionTimeout,
				authPasswordPolicy: formData.authPasswordPolicy,
				oidcEnabled: formData.oidcEnabled,
				oidcMergeAccounts: formData.oidcMergeAccounts,
				oidcSkipTlsVerify: formData.oidcSkipTlsVerify,
				oidcAutoRedirectToProvider: formData.oidcAutoRedirectToProvider,
				oidcClientId: formData.oidcClientId,
				oidcIssuerUrl: formData.oidcIssuerUrl,
				oidcScopes: formData.oidcScopes,
				oidcGroupsClaim: formData.oidcGroupsClaim,
				oidcProviderName: formData.oidcProviderName,
				oidcProviderLogoUrl: formData.oidcProviderLogoUrl,
				...(formData.oidcClientSecret && { oidcClientSecret: formData.oidcClientSecret })
			});
			$formInputs.oidcClientSecret.value = '';
			toast.success(m.security_settings_saved());
		} catch (error: any) {
			console.error('Failed to save settings:', error);
			toast.error(m.security_settings_save_failed());
		} finally {
			settingsForm.setLoading(false);
		}
	}

	function customReset() {
		form.reset();
		$formInputs.oidcClientSecret.value = '';
	}

	function handleLocalSwitchChange(checked: boolean) {
		if (!checked && !isOidcEnabledForAuthValidation) {
			$formInputs.authLocalEnabled.value = true;
			toast.error(m.security_enable_one_provider_error());
			return;
		}
		$formInputs.authLocalEnabled.value = checked;
	}

	function handleOidcEnabledChange(checked: boolean) {
		if (!checked && !$formInputs.authLocalEnabled.value && !isOidcEnvForced) {
			$formInputs.authLocalEnabled.value = true;
			toast.info(m.security_local_enabled_info());
		}
		$formInputs.oidcEnabled.value = checked;
	}

	function handleMergeAccountsChange(checked: boolean) {
		if (checked && !currentSettings.oidcMergeAccounts) {
			showMergeAccountsAlert = true;
		} else {
			$formInputs.oidcMergeAccounts.value = checked;
		}
	}

	function confirmMergeAccounts() {
		$formInputs.oidcMergeAccounts.value = true;
		showMergeAccountsAlert = false;
	}

	function cancelMergeAccounts() {
		$formInputs.oidcMergeAccounts.value = false;
		showMergeAccountsAlert = false;
	}

	$effect(() => {
		settingsForm.registerFormActions(customSubmit, customReset);
		const formState = getContext('settingsFormState') as any;
		if (formState) {
			formState.hasChanges = hasAuthenticationChanges;
		}
	});
</script>

<SettingsPageLayout
	title={m.authentication_title()}
	description={m.authentication_description()}
	icon={LockIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.security_authentication_heading()}</h3>

				{#if isAutoLoginEnabled}
					<Alert.Root variant="default" class="border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950">
						<InfoIcon class="h-4 w-4 text-amber-600 dark:text-amber-500" />
						<Alert.Title class="text-amber-900 dark:text-amber-100">{m.security_auto_login_enabled_title()}</Alert.Title>
						<Alert.Description class="text-amber-800 dark:text-amber-200">
							{m.security_auto_login_enabled_description()}
						</Alert.Description>
					</Alert.Root>
				{:else}
					<div class="divide-border/40 divide-y [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
						<SettingsRow label={m.security_local_auth_label()} description={m.security_local_auth_description()} layout="inline">
							<Switch
								id="localAuthSwitch"
								bind:checked={$formInputs.authLocalEnabled.value}
								onCheckedChange={handleLocalSwitchChange}
							/>
						</SettingsRow>

						<Collapsible.Root bind:open={oidcConfigOpen}>
							<SettingsRow label={m.security_oidc_auth_label()} description={m.security_oidc_auth_description()} layout="inline">
								{#snippet labelExtra()}
									{#if isOidcEnvForced}
										<div class="mt-2">
											<ArcaneTooltip.Root>
												<ArcaneTooltip.Trigger>
													<span
														class="inline-flex items-center gap-1.5 rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800 ring-1 ring-amber-200 dark:bg-amber-900/50 dark:text-amber-200 dark:ring-amber-800"
													>
														{#if isOidcForcedDisabled}
															{m.security_server_disabled_via_server()}
														{:else}
															{m.security_server_configured()}
														{/if}
													</span>
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
										bind:checked={$formInputs.oidcEnabled.value}
										onCheckedChange={handleOidcEnabledChange}
									/>
									{#if showOidcDetails}
										<Collapsible.Trigger
											class="text-muted-foreground hover:text-foreground hover:bg-muted/50 inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium transition-colors"
										>
											<span>{oidcConfigOpen ? m.common_hide() : m.common_show()} {m.common_configuration()}</span>
											<ArrowDownIcon class={cn('size-3.5 transition-transform', oidcConfigOpen && 'rotate-180')} />
										</Collapsible.Trigger>
									{/if}
								</div>
							</SettingsRow>

							{#if showOidcDetails}
								<Collapsible.Content class="mt-4">
									<div class="border-border/60 space-y-5 border-l-2 pl-5">
										<div class="grid gap-5 sm:grid-cols-2">
											<TextInputWithLabel
												id="oidcClientId"
												label={m.oidc_client_id_label()}
												placeholder={m.oidc_client_id_placeholder()}
												disabled={isOidcEnvForced}
												bind:value={$formInputs.oidcClientId.value}
												error={$formInputs.oidcClientId.error}
											/>
											<TextInputWithLabel
												id="oidcClientSecret"
												type="password"
												label={m.oidc_client_secret_label()}
												placeholder={m.oidc_client_secret_placeholder()}
												disabled={isOidcEnvForced}
												bind:value={$formInputs.oidcClientSecret.value}
												error={$formInputs.oidcClientSecret.error}
												helpText={m.security_oidc_client_secret_help()}
											/>
										</div>

										<TextInputWithLabel
											id="oidcIssuerUrl"
											label={m.oidc_issuer_url_label()}
											description={m.oidc_issuer_url_description()}
											placeholder={m.oidc_issuer_url_placeholder()}
											disabled={isOidcEnvForced}
											bind:value={$formInputs.oidcIssuerUrl.value}
											error={$formInputs.oidcIssuerUrl.error}
										/>

										<div class="grid gap-5 sm:grid-cols-2">
											<TextInputWithLabel
												id="oidcProviderName"
												label={m.oidc_provider_name_label()}
												description={m.oidc_provider_name_description()}
												placeholder={m.oidc_provider_name_placeholder()}
												disabled={isOidcEnvForced}
												bind:value={$formInputs.oidcProviderName.value}
												error={$formInputs.oidcProviderName.error}
											/>
											<TextInputWithLabel
												id="oidcProviderLogoUrl"
												label={m.oidc_provider_logo_url_label()}
												description={m.oidc_provider_logo_url_description()}
												placeholder={m.oidc_provider_logo_url_placeholder()}
												disabled={isOidcEnvForced}
												bind:value={$formInputs.oidcProviderLogoUrl.value}
												error={$formInputs.oidcProviderLogoUrl.error}
											/>
										</div>

										<TextInputWithLabel
											id="oidcScopes"
											label={m.oidc_scopes_label()}
											placeholder={m.oidc_scopes_placeholder()}
											disabled={isOidcEnvForced}
											bind:value={$formInputs.oidcScopes.value}
											error={$formInputs.oidcScopes.error}
										/>

										<TextInputWithLabel
											id="oidcGroupsClaim"
											label={m.oidc_groups_claim_label()}
											placeholder={m.oidc_groups_claim_placeholder()}
											disabled={isOidcEnvForced}
											bind:value={$formInputs.oidcGroupsClaim.value}
											error={$formInputs.oidcGroupsClaim.error}
											helpText={m.oidc_groups_claim_help()}
										/>

										<div class="divide-border/40 divide-y pt-2 [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
											<SettingsRow
												label={m.security_oidc_merge_accounts_label()}
												description={m.security_oidc_merge_accounts_description()}
												layout="inline"
											>
												<Switch
													id="oidcMergeAccountsSwitch"
													disabled={isOidcEnvForced}
													bind:checked={$formInputs.oidcMergeAccounts.value}
													onCheckedChange={handleMergeAccountsChange}
												/>
											</SettingsRow>

											<SettingsRow
												label={m.oidc_skip_tls_verify_label()}
												description={m.oidc_skip_tls_verify_description()}
												layout="inline"
											>
												<Switch
													id="oidcSkipTlsVerifySwitch"
													disabled={isOidcEnvForced}
													bind:checked={$formInputs.oidcSkipTlsVerify.value}
												/>
											</SettingsRow>

											<SettingsRow
												label={m.oidc_auto_redirect_label()}
												description={m.oidc_auto_redirect_description()}
												layout="inline"
											>
												<Switch
													id="oidcAutoRedirectSwitch"
													disabled={isOidcEnvForced}
													bind:checked={$formInputs.oidcAutoRedirectToProvider.value}
												/>
											</SettingsRow>
										</div>

										<div class="bg-muted/30 rounded-lg border p-4">
											<div class="mb-2 flex items-center gap-2">
												<InfoIcon class="size-4 text-blue-600" />
												<span class="text-sm font-medium">{m.oidc_redirect_uri_title()}</span>
											</div>
											<p class="text-muted-foreground mb-3 text-sm">{m.oidc_redirect_uri_description()}</p>
											<div class="flex items-center gap-2">
												<code class="bg-muted flex-1 rounded p-2 font-mono text-xs break-all">{redirectUri}</code>
												<CopyButton text={redirectUri} size="sm" variant="outline" class="shrink-0" title={m.common_copy()} />
											</div>
										</div>
									</div>
								</Collapsible.Content>
							{/if}
						</Collapsible.Root>
					</div>

					<IfPermitted adminOnly>
						<div class="border-border/40 space-y-4 border-t pt-6">
							<div class="flex items-start justify-between gap-4">
								<div>
									<h4 class="text-sm font-semibold">{m.oidc_role_mappings_title()}</h4>
									<p class="text-muted-foreground mt-0.5 text-xs">{m.oidc_role_mappings_description()}</p>
								</div>
								<ArcaneButton
									action="create"
									tone="outline"
									size="sm"
									onclick={openCreateMapping}
									customLabel={m.common_create_button({ resource: m.resource_oidc_mapping_cap() })}
								/>
							</div>
							{#if oidcMappings.length > 0}
								<OidcMappingTable
									mappings={oidcMappings}
									roles={data.roles}
									environments={data.environments}
									onRefresh={refreshMappings}
									onEdit={openEditMapping}
								/>
							{:else}
								<div class="border-border/40 bg-muted/20 rounded-lg border border-dashed p-6 text-center">
									<p class="text-muted-foreground text-sm">{m.oidc_mappings_empty_body()}</p>
								</div>
							{/if}
						</div>
					</IfPermitted>
				{/if}
			</div>

			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.security_session_heading()}</h3>
				<div class="max-w-xs">
					<TextInputWithLabel
						id="authSessionTimeout"
						type="number"
						label={m.security_session_timeout_label()}
						description={m.security_session_timeout_description()}
						bind:value={$formInputs.authSessionTimeout.value}
						error={$formInputs.authSessionTimeout.error}
					/>
				</div>
			</div>

			<div class="space-y-4">
				<h3 class="text-base font-semibold">{m.security_password_policy_label()}</h3>
				<SettingsRow label={m.security_password_policy_label()} description={m.security_password_policy_description()}>
					<div class="grid grid-cols-1 gap-2 sm:grid-cols-3 sm:gap-3" role="group" aria-labelledby="passwordPolicyLabel">
						<ArcaneTooltip.Root>
							<ArcaneTooltip.Trigger>
								<ArcaneButton
									action="base"
									tone={$formInputs.authPasswordPolicy.value === 'basic' ? 'outline-primary' : 'outline'}
									class="h-12 w-full text-xs sm:text-sm"
									onclick={() => ($formInputs.authPasswordPolicy.value = 'basic')}
									customLabel={m.common_basic()}
								/>
							</ArcaneTooltip.Trigger>
							<ArcaneTooltip.Content side="top">
								{m.security_password_policy_basic_tooltip()}
							</ArcaneTooltip.Content>
						</ArcaneTooltip.Root>

						<ArcaneTooltip.Root>
							<ArcaneTooltip.Trigger>
								<ArcaneButton
									action="base"
									tone={$formInputs.authPasswordPolicy.value === 'standard' ? 'outline-primary' : 'outline'}
									class="h-12 w-full text-xs sm:text-sm"
									onclick={() => ($formInputs.authPasswordPolicy.value = 'standard')}
									customLabel={m.security_password_policy_standard()}
								/>
							</ArcaneTooltip.Trigger>
							<ArcaneTooltip.Content side="top">
								{m.security_password_policy_standard_tooltip()}
							</ArcaneTooltip.Content>
						</ArcaneTooltip.Root>

						<ArcaneTooltip.Root>
							<ArcaneTooltip.Trigger>
								<ArcaneButton
									action="base"
									tone={$formInputs.authPasswordPolicy.value === 'strong' ? 'outline-primary' : 'outline'}
									class="h-12 w-full text-xs sm:text-sm"
									onclick={() => ($formInputs.authPasswordPolicy.value = 'strong')}
									customLabel={m.security_password_policy_strong()}
								/>
							</ArcaneTooltip.Trigger>
							<ArcaneTooltip.Content side="top">
								{m.security_password_policy_strong_tooltip()}
							</ArcaneTooltip.Content>
						</ArcaneTooltip.Root>
					</div>
				</SettingsRow>
			</div>
		</fieldset>
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
