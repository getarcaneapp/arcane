<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { z } from 'zod/v4';
	import { onMount } from 'svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import DropdownCard from '$lib/components/dropdown-card.svelte';
	import { toast } from 'svelte-sonner';
	import type { PageData } from './$types';
	import type { Settings } from '$lib/types/settings.type';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { m } from '$lib/paraglide/messages';
	import LockIcon from '@lucide/svelte/icons/lock';
	import ClockIcon from '@lucide/svelte/icons/clock';
	import KeyIcon from '@lucide/svelte/icons/key';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import InfoIcon from '@lucide/svelte/icons/info';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsPageLayout } from '$lib/layouts';
	import { UseClipboard } from '$lib/hooks/use-clipboard.svelte';
	import { createSettingsForm } from '$lib/utils/settings-form.util';

	let { data }: { data: PageData } = $props();
	const currentSettings = $derived<Settings>($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);

	const formSchema = z
		.object({
			authLocalEnabled: z.boolean(),
			authSessionTimeout: z
				.number(m.security_session_timeout_required())
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
	let { formInputs, form, settingsForm, registerOnMount } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings: formDefaults,
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

	const clipboard = new UseClipboard();
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

	function handleCopy(text?: string) {
		if (!text) return;
		clipboard.copy(text);
	}

	onMount(() => {
		// Use custom submit/reset for security page
		settingsForm.registerFormActions(customSubmit, customReset);
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
		<fieldset disabled={isReadOnly} class="relative">
			<div class="space-y-4 sm:space-y-6">
				<Card.Root>
					<Card.Header icon={LockIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.security_authentication_heading()}</Card.Title>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<SwitchWithLabel
							id="localAuthSwitch"
							label={m.security_local_auth_label()}
							description={m.security_local_auth_description()}
							error={$formInputs.authLocalEnabled.error}
							bind:checked={$formInputs.authLocalEnabled.value}
							onCheckedChange={handleLocalSwitchChange}
						/>
					</Card.Content>
				</Card.Root>

				<DropdownCard
					id="oidc-settings"
					title={m.security_oidc_auth_label()}
					description={m.security_oidc_auth_description()}
					icon={ShieldCheckIcon}
					defaultExpanded={currentSettings.oidcEnabled || isOidcEnvForced}
				>
					{#snippet badge()}
						{#if isOidcEnvForced}
							<span
								class="inline-flex items-center gap-1.5 rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800 ring-1 ring-amber-200 dark:bg-amber-900/50 dark:text-amber-200 dark:ring-amber-800"
							>
								{m.security_server_configured()}
							</span>
						{/if}
					{/snippet}
					<div class="space-y-4">
						<SwitchWithLabel
							id="oidcEnabledSwitch"
							label={m.security_oidc_enabled_label()}
							description={m.security_oidc_enabled_description()}
							disabled={isOidcEnvForced}
							bind:checked={$formInputs.oidcEnabled.value}
							onCheckedChange={handleOidcEnabledChange}
						/>

						<div class="space-y-4 border-t pt-4">
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
									</div>
								</div>
							</div>

							<div class="border-t pt-4">
								<SwitchWithLabel
									id="oidcMergeAccountsSwitch"
									label={m.security_oidc_merge_accounts_label()}
									description={m.security_oidc_merge_accounts_description()}
									disabled={isOidcEnvForced}
									bind:checked={$formInputs.oidcMergeAccounts.value}
									onCheckedChange={handleMergeAccountsChange}
								/>
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
								<Button
									size="sm"
									variant="outline"
									onclick={() => handleCopy(redirectUri)}
									class="shrink-0"
									title={m.common_copy()}
								>
									<CopyIcon class="size-3" />
								</Button>
							</div>
						</div>
					</div></DropdownCard
				>

				<Card.Root>
					<Card.Header icon={ClockIcon} class="items-start">
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.security_session_heading()}</Card.Title>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<TextInputWithLabel
							bind:value={$formInputs.authSessionTimeout.value}
							error={$formInputs.authSessionTimeout.error}
							label={m.security_session_timeout_label()}
							placeholder={m.security_session_timeout_placeholder()}
							helpText={m.security_session_timeout_description()}
							type="number"
						/>
					</Card.Content>
				</Card.Root>

				<Card.Root>
					<Card.Header icon={KeyIcon} class="items-start">
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.security_password_policy_label()}</Card.Title>
							<Card.Description>{m.security_password_policy_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<Tooltip.Provider>
							<div class="grid grid-cols-1 gap-2 sm:grid-cols-3 sm:gap-3" role="group" aria-labelledby="passwordPolicyLabel">
								<Tooltip.Root>
									<Tooltip.Trigger>
										<Button
											variant={$formInputs.authPasswordPolicy.value === 'basic' ? 'default' : 'outline'}
											class={$formInputs.authPasswordPolicy.value === 'basic'
												? 'arcane-button-create h-12 w-full text-xs sm:text-sm'
												: 'arcane-button-restart h-12 w-full text-xs sm:text-sm'}
											onclick={() => ($formInputs.authPasswordPolicy.value = 'basic')}
											type="button"
											>{m.common_basic()}
										</Button>
									</Tooltip.Trigger>
									<Tooltip.Content side="top" align="center">{m.security_password_policy_basic_tooltip()}</Tooltip.Content>
								</Tooltip.Root>

								<Tooltip.Root>
									<Tooltip.Trigger>
										<Button
											variant={$formInputs.authPasswordPolicy.value === 'standard' ? 'default' : 'outline'}
											class={$formInputs.authPasswordPolicy.value === 'standard'
												? 'arcane-button-create h-12 w-full text-xs sm:text-sm'
												: 'arcane-button-restart h-12 w-full text-xs sm:text-sm'}
											onclick={() => ($formInputs.authPasswordPolicy.value = 'standard')}
											type="button"
											>{m.security_password_policy_standard()}
										</Button>
									</Tooltip.Trigger>
									<Tooltip.Content side="top" align="center">{m.security_password_policy_standard_tooltip()}</Tooltip.Content>
								</Tooltip.Root>

								<Tooltip.Root>
									<Tooltip.Trigger>
										<Button
											variant={$formInputs.authPasswordPolicy.value === 'strong' ? 'default' : 'outline'}
											class={$formInputs.authPasswordPolicy.value === 'strong'
												? 'arcane-button-create h-12 w-full text-xs sm:text-sm'
												: 'arcane-button-restart h-12 w-full text-xs sm:text-sm'}
											onclick={() => ($formInputs.authPasswordPolicy.value = 'strong')}
											type="button"
											>{m.security_password_policy_strong()}
										</Button>
									</Tooltip.Trigger>
									<Tooltip.Content side="top" align="center">{m.security_password_policy_strong_tooltip()}</Tooltip.Content>
								</Tooltip.Root>
							</div>
						</Tooltip.Provider>
					</Card.Content>
				</Card.Root>
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
