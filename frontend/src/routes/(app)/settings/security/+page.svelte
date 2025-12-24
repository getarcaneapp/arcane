<script lang="ts">
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { z } from 'zod/v4';
	import { getContext } from 'svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { Separator } from '$lib/components/ui/separator';
	import { toast } from 'svelte-sonner';
	import type { PageData } from './$types';
	import type { Settings } from '$lib/types/settings.type';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import { m } from '$lib/paraglide/messages';
	import { LockIcon, InfoIcon } from '$lib/icons';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsPageLayout } from '$lib/layouts';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { createSettingsForm } from '$lib/utils/settings-form.util';

	let { data }: { data: PageData } = $props();
	const currentSettings = $derived<Settings>($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);

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
			oidcClientId: z.string(),
			oidcClientSecret: z.string(),
			oidcIssuerUrl: z.string(),
			oidcScopes: z.string(),
			oidcAdminClaim: z.string(),
			oidcAdminValue: z.string()
		})
		.superRefine((formData, ctx) => {
			if (data.oidcStatus.envForced || formData.oidcEnabled) return;
			if (!formData.authLocalEnabled) {
				ctx.addIssue({
					code: 'custom',
					message: m.security_enable_one_provider(),
					path: ['authLocalEnabled']
				});
			}
		});

	let showMergeAccountsAlert = $state(false);

	const formDefaults = $derived({
		authLocalEnabled: currentSettings.authLocalEnabled,
		authSessionTimeout: currentSettings.authSessionTimeout,
		authPasswordPolicy: currentSettings.authPasswordPolicy,
		oidcEnabled: currentSettings.oidcEnabled,
		oidcMergeAccounts: currentSettings.oidcMergeAccounts,
		oidcClientId: currentSettings.oidcClientId,
		oidcClientSecret: '',
		oidcIssuerUrl: currentSettings.oidcIssuerUrl,
		oidcScopes: currentSettings.oidcScopes,
		oidcAdminClaim: currentSettings.oidcAdminClaim,
		oidcAdminValue: currentSettings.oidcAdminValue
	});

	// Security page needs custom submit logic for OIDC client secret handling
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
				oidcClientId: ($settingsStore || data.settings!).oidcClientId,
				oidcClientSecret: '',
				oidcIssuerUrl: ($settingsStore || data.settings!).oidcIssuerUrl,
				oidcScopes: ($settingsStore || data.settings!).oidcScopes,
				oidcAdminClaim: ($settingsStore || data.settings!).oidcAdminClaim,
				oidcAdminValue: ($settingsStore || data.settings!).oidcAdminValue
			}),
			successMessage: m.security_settings_saved()
		})
	);

	// Override the default hasChanges since we need special handling for oidcClientSecret
	const hasSecurityChanges = $derived(
		$formInputs.authLocalEnabled.value !== currentSettings.authLocalEnabled ||
			$formInputs.authSessionTimeout.value !== currentSettings.authSessionTimeout ||
			$formInputs.authPasswordPolicy.value !== currentSettings.authPasswordPolicy ||
			$formInputs.oidcEnabled.value !== currentSettings.oidcEnabled ||
			$formInputs.oidcMergeAccounts.value !== currentSettings.oidcMergeAccounts ||
			$formInputs.oidcClientId.value !== currentSettings.oidcClientId ||
			$formInputs.oidcIssuerUrl.value !== currentSettings.oidcIssuerUrl ||
			$formInputs.oidcScopes.value !== currentSettings.oidcScopes ||
			$formInputs.oidcAdminClaim.value !== currentSettings.oidcAdminClaim ||
			$formInputs.oidcAdminValue.value !== currentSettings.oidcAdminValue ||
			$formInputs.oidcClientSecret.value !== ''
	);

	const redirectUri = $derived(`${globalThis?.location?.origin ?? ''}/auth/oidc/callback`);
	const isOidcEnvForced = $derived(data.oidcStatus.envForced);

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
				oidcClientId: formData.oidcClientId,
				oidcIssuerUrl: formData.oidcIssuerUrl,
				oidcScopes: formData.oidcScopes,
				oidcAdminClaim: formData.oidcAdminClaim,
				oidcAdminValue: formData.oidcAdminValue,
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
		if (!checked && !$formInputs.oidcEnabled.value && !data.oidcStatus.envForced) {
			$formInputs.authLocalEnabled.value = true;
			toast.error(m.security_enable_one_provider_error());
			return;
		}
		$formInputs.authLocalEnabled.value = checked;
	}

	function handleOidcEnabledChange(checked: boolean) {
		if (!checked && !$formInputs.authLocalEnabled.value && !data.oidcStatus.envForced) {
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
		// Use custom submit/reset for security page
		settingsForm.registerFormActions(customSubmit, customReset);
		// Sync the custom hasSecurityChanges to the context
		const formState = getContext('settingsFormState') as any;
		if (formState) {
			formState.hasChanges = hasSecurityChanges;
		}
	});
</script>

<SettingsPageLayout
	title={m.security_title()}
	description={m.security_description()}
	icon={LockIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative space-y-8">
			<!-- Authentication Section -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.security_authentication_heading()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<!-- Local Auth -->
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.security_local_auth_label()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.security_local_auth_description()}</p>
							</div>
							<div class="flex items-center gap-2">
								<Switch
									id="localAuthSwitch"
									bind:checked={$formInputs.authLocalEnabled.value}
									onCheckedChange={handleLocalSwitchChange}
								/>
								<Label for="localAuthSwitch" class="font-normal">
									{$formInputs.authLocalEnabled.value ? m.common_enabled() : m.common_disabled()}
								</Label>
							</div>
						</div>

						<Separator />

						<!-- OIDC Auth -->
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.security_oidc_auth_label()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.security_oidc_auth_description()}</p>
								{#if isOidcEnvForced}
									<div class="mt-2">
										<span
											class="inline-flex items-center gap-1.5 rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800 ring-1 ring-amber-200 dark:bg-amber-900/50 dark:text-amber-200 dark:ring-amber-800"
										>
											{m.security_server_configured()}
										</span>
									</div>
								{/if}
							</div>
							<div class="space-y-4">
								<div class="flex items-center gap-2">
									<Switch
										id="oidcEnabledSwitch"
										disabled={isOidcEnvForced}
										bind:checked={$formInputs.oidcEnabled.value}
										onCheckedChange={handleOidcEnabledChange}
									/>
									<Label for="oidcEnabledSwitch" class="font-normal">
										{m.security_oidc_enabled_label()}
									</Label>
								</div>

								{#if $formInputs.oidcEnabled.value || isOidcEnvForced}
									<div class="space-y-4 pt-2">
										<div class="space-y-2">
											<Label for="oidcClientId" class="text-sm font-medium">{m.oidc_client_id_label()}</Label>
											<Input
												id="oidcClientId"
												type="text"
												placeholder={m.oidc_client_id_placeholder()}
												disabled={isOidcEnvForced}
												bind:value={$formInputs.oidcClientId.value}
												class="font-mono text-sm"
											/>
											{#if $formInputs.oidcClientId.error}
												<p class="text-destructive text-[0.8rem] font-medium">{$formInputs.oidcClientId.error}</p>
											{/if}
										</div>

										<div class="space-y-2">
											<Label for="oidcClientSecret" class="text-sm font-medium">{m.oidc_client_secret_label()}</Label>
											<Input
												id="oidcClientSecret"
												type="password"
												placeholder={m.oidc_client_secret_placeholder()}
												disabled={isOidcEnvForced}
												bind:value={$formInputs.oidcClientSecret.value}
												class="font-mono text-sm"
											/>
											<p class="text-muted-foreground text-xs">{m.security_oidc_client_secret_help()}</p>
											{#if $formInputs.oidcClientSecret.error}
												<p class="text-destructive text-[0.8rem] font-medium">{$formInputs.oidcClientSecret.error}</p>
											{/if}
										</div>

										<div class="space-y-2">
											<Label for="oidcIssuerUrl" class="text-sm font-medium">{m.oidc_issuer_url_label()}</Label>
											<Input
												id="oidcIssuerUrl"
												type="text"
												placeholder={m.oidc_issuer_url_placeholder()}
												disabled={isOidcEnvForced}
												bind:value={$formInputs.oidcIssuerUrl.value}
												class="font-mono text-sm"
											/>
											<p class="text-muted-foreground text-xs">{m.oidc_issuer_url_description()}</p>
											{#if $formInputs.oidcIssuerUrl.error}
												<p class="text-destructive text-[0.8rem] font-medium">{$formInputs.oidcIssuerUrl.error}</p>
											{/if}
										</div>

										<div class="space-y-2">
											<Label for="oidcScopes" class="text-sm font-medium">{m.oidc_scopes_label()}</Label>
											<Input
												id="oidcScopes"
												type="text"
												placeholder={m.oidc_scopes_placeholder()}
												disabled={isOidcEnvForced}
												bind:value={$formInputs.oidcScopes.value}
												class="font-mono text-sm"
											/>
											{#if $formInputs.oidcScopes.error}
												<p class="text-destructive text-[0.8rem] font-medium">{$formInputs.oidcScopes.error}</p>
											{/if}
										</div>

										<div class="border-t pt-4">
											<h4 class="text-sm font-semibold">{m.oidc_admin_role_mapping_title()}</h4>
											<p class="text-muted-foreground mb-3 text-xs">{m.oidc_admin_role_mapping_description()}</p>
											<div class="grid gap-3 sm:grid-cols-2">
												<div class="space-y-2">
													<Label for="oidcAdminClaim" class="text-sm font-medium">{m.oidc_admin_claim_label()}</Label>
													<Input
														id="oidcAdminClaim"
														type="text"
														placeholder={m.oidc_admin_claim_placeholder()}
														disabled={isOidcEnvForced}
														bind:value={$formInputs.oidcAdminClaim.value}
														class="font-mono text-sm"
													/>
													{#if $formInputs.oidcAdminClaim.error}
														<p class="text-destructive text-[0.8rem] font-medium">{$formInputs.oidcAdminClaim.error}</p>
													{/if}
												</div>
												<div class="space-y-2">
													<Label for="oidcAdminValue" class="text-sm font-medium">{m.oidc_admin_value_label()}</Label>
													<Input
														id="oidcAdminValue"
														type="text"
														placeholder={m.oidc_admin_value_placeholder()}
														disabled={isOidcEnvForced}
														bind:value={$formInputs.oidcAdminValue.value}
														class="font-mono text-sm"
													/>
													<p class="text-muted-foreground text-[11px]">{m.oidc_admin_value_help()}</p>
													{#if $formInputs.oidcAdminValue.error}
														<p class="text-destructive text-[0.8rem] font-medium">{$formInputs.oidcAdminValue.error}</p>
													{/if}
												</div>
											</div>
										</div>

										<div class="border-t pt-4">
											<div class="flex items-center gap-2">
												<Switch
													id="oidcMergeAccountsSwitch"
													disabled={isOidcEnvForced}
													bind:checked={$formInputs.oidcMergeAccounts.value}
													onCheckedChange={handleMergeAccountsChange}
												/>
												<div class="grid gap-1.5 leading-none">
													<Label for="oidcMergeAccountsSwitch" class="font-normal">
														{m.security_oidc_merge_accounts_label()}
													</Label>
													<p class="text-muted-foreground text-xs">
														{m.security_oidc_merge_accounts_description()}
													</p>
												</div>
											</div>
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
								{/if}
							</div>
						</div>
					</div>
				</div>
			</div>

			<!-- Session Section -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.security_session_heading()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.security_session_timeout_label()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.security_session_timeout_description()}</p>
							</div>
							<div class="max-w-xs">
								<TextInputWithLabel
									bind:value={$formInputs.authSessionTimeout.value}
									error={$formInputs.authSessionTimeout.error}
									label={m.security_session_timeout_label()}
									placeholder={m.security_session_timeout_placeholder()}
									helpText={m.security_session_timeout_description()}
									type="number"
								/>
							</div>
						</div>
					</div>
				</div>
			</div>

			<!-- Password Policy Section -->
			<div class="space-y-4">
				<h3 class="text-lg font-medium">{m.security_password_policy_label()}</h3>
				<div class="bg-card rounded-lg border shadow-sm">
					<div class="space-y-6 p-6">
						<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
							<div>
								<Label class="text-base">{m.security_password_policy_label()}</Label>
								<p class="text-muted-foreground mt-1 text-sm">{m.security_password_policy_description()}</p>
							</div>
							<div>
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
							</div>
						</div>
					</div>
				</div>
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
	{/snippet}
</SettingsPageLayout>
