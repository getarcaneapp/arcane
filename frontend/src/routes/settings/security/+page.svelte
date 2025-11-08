<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import { z } from 'zod/v4';
	import { onMount } from 'svelte';
	import { createForm } from '$lib/utils/form.utils';
	import { Button } from '$lib/components/ui/button';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import OidcConfigDialog from '$lib/components/dialogs/oidc-config-dialog.svelte';
	import { toast } from 'svelte-sonner';
	import type { PageData } from './$types';
	import type { Settings } from '$lib/types/settings.type';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import { m } from '$lib/paraglide/messages';
	import LockIcon from '@lucide/svelte/icons/lock';
	import ClockIcon from '@lucide/svelte/icons/clock';
	import KeyIcon from '@lucide/svelte/icons/key';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { SettingsPageLayout } from '$lib/layouts';
	import { UseSettingsForm } from '$lib/hooks/use-settings-form.svelte';

	let { data }: { data: PageData } = $props();
	const currentSettings = $derived<Settings>($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);

	const formSchema = z
		.object({
			authLocalEnabled: z.boolean(),
			authOidcEnabled: z.boolean(),
			authOidcMergeAccounts: z.boolean(),
			authSessionTimeout: z
				.number(m.security_session_timeout_required())
				.int(m.security_session_timeout_integer())
				.min(15, m.security_session_timeout_min())
				.max(1440, m.security_session_timeout_max()),
			authPasswordPolicy: z.enum(['basic', 'standard', 'strong'])
		})
		.superRefine((formData, ctx) => {
			// If server forces OIDC, the constraint is already satisfied
			if (data.oidcStatus.envForced) return;
			if (!formData.authLocalEnabled && !formData.authOidcEnabled) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					message: m.security_enable_one_provider(),
					path: ['authLocalEnabled']
				});
			}
		});

	let showOidcConfigDialog = $state(false);
	let showMergeAccountsAlert = $state(false);

	let oidcConfigForm = $state({
		clientId: '',
		clientSecret: '',
		issuerUrl: '',
		scopes: 'openid email profile',
		adminClaim: '',
		adminValue: ''
	});

	let { inputs: formInputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, currentSettings));

	const settingsForm = new UseSettingsForm({
		hasChangesChecker: () =>
			$formInputs.authLocalEnabled.value !== currentSettings.authLocalEnabled ||
			$formInputs.authOidcEnabled.value !== currentSettings.authOidcEnabled ||
			$formInputs.authOidcMergeAccounts.value !== currentSettings.authOidcMergeAccounts ||
			$formInputs.authSessionTimeout.value !== currentSettings.authSessionTimeout ||
			$formInputs.authPasswordPolicy.value !== currentSettings.authPasswordPolicy
	});

	// Helper: treat OIDC as active if forced by server or enabled in form
	const isOidcActive = () => $formInputs.authOidcEnabled.value || data.oidcStatus.envForced;

	async function onSubmit() {
		const formData = form.validate();
		if (!formData) {
			toast.error('Please check the form for errors');
			return;
		}

		settingsForm.setLoading(true);

		let authOidcConfig = currentSettings.authOidcConfig;
		if (formData.authOidcEnabled && !data.oidcStatus.envForced) {
			authOidcConfig = JSON.stringify({
				clientId: oidcConfigForm.clientId,
				clientSecret: oidcConfigForm.clientSecret || '',
				issuerUrl: oidcConfigForm.issuerUrl,
				scopes: oidcConfigForm.scopes,
				adminClaim: oidcConfigForm.adminClaim || '',
				adminValue: oidcConfigForm.adminValue || ''
			});
		}

		await settingsForm
			.updateSettings({
				authLocalEnabled: formData.authLocalEnabled,
				authOidcEnabled: formData.authOidcEnabled,
				authOidcMergeAccounts: formData.authOidcMergeAccounts,
				authSessionTimeout: formData.authSessionTimeout,
				authPasswordPolicy: formData.authPasswordPolicy,
				...(formData.authOidcEnabled && !data.oidcStatus.envForced && { authOidcConfig })
			})
			.then(() => toast.success(m.security_settings_saved()))
			.catch((error: any) => {
				console.error('Failed to save settings:', error);
				const errorMessage = error?.response?.data?.error || error?.message || 'Failed to save settings. Please try again.';
				toast.error(errorMessage);
			})
			.finally(() => settingsForm.setLoading(false));
	}

	function resetForm() {
		$formInputs.authLocalEnabled.value = currentSettings.authLocalEnabled;
		$formInputs.authOidcEnabled.value = currentSettings.authOidcEnabled;
		$formInputs.authOidcMergeAccounts.value = currentSettings.authOidcMergeAccounts;
		$formInputs.authSessionTimeout.value = currentSettings.authSessionTimeout;
		$formInputs.authPasswordPolicy.value = currentSettings.authPasswordPolicy;
	}

	// Only depend on envForced; open config when enabling and not forced
	function handleOidcSwitchChange(checked: boolean) {
		$formInputs.authOidcEnabled.value = checked;

		if (!checked && !$formInputs.authLocalEnabled.value && !data.oidcStatus.envForced) {
			$formInputs.authLocalEnabled.value = true;
			toast.info(m.security_local_enabled_info());
		}

		if (checked && !data.oidcStatus.envForced) {
			showOidcConfigDialog = true;
		}
	}

	function handleLocalSwitchChange(checked: boolean) {
		if (!checked && !isOidcActive()) {
			$formInputs.authLocalEnabled.value = true;
			toast.error(m.security_enable_one_provider_error());
			return;
		}
		$formInputs.authLocalEnabled.value = checked;
	}

	function handleMergeAccountsChange(checked: boolean) {
		if (checked && !currentSettings.authOidcMergeAccounts) {
			showMergeAccountsAlert = true;
		} else {
			$formInputs.authOidcMergeAccounts.value = checked;
		}
	}

	function confirmMergeAccounts() {
		$formInputs.authOidcMergeAccounts.value = true;
		showMergeAccountsAlert = false;
	}

	function cancelMergeAccounts() {
		$formInputs.authOidcMergeAccounts.value = false;
		showMergeAccountsAlert = false;
	}

	function openOidcDialog() {
		if (currentSettings.authOidcConfig) {
			const cfg = JSON.parse(currentSettings.authOidcConfig);
			oidcConfigForm.clientId = cfg.clientId || '';
			oidcConfigForm.issuerUrl = cfg.issuerUrl || '';
			oidcConfigForm.scopes = cfg.scopes || 'openid email profile';
			oidcConfigForm.adminClaim = cfg.adminClaim || '';
			oidcConfigForm.adminValue = cfg.adminValue || '';
		}
		oidcConfigForm.clientSecret = '';
		showOidcConfigDialog = true;
	}

	async function handleSaveOidcConfig() {
		try {
			settingsForm.setLoading(true);
			$formInputs.authOidcEnabled.value = true;

			const formData = form.validate();
			if (!formData) {
				settingsForm.setLoading(false);
				return;
			}

			const authOidcConfig = JSON.stringify({
				clientId: oidcConfigForm.clientId,
				clientSecret: oidcConfigForm.clientSecret || '',
				issuerUrl: oidcConfigForm.issuerUrl,
				scopes: oidcConfigForm.scopes,
				adminClaim: oidcConfigForm.adminClaim || '',
				adminValue: oidcConfigForm.adminValue || ''
			});

			await settingsForm.updateSettings({
				authOidcEnabled: true,
				authOidcConfig
			});

			toast.success(m.security_oidc_saved());
			showOidcConfigDialog = false;
		} finally {
			settingsForm.setLoading(false);
		}
	}

	onMount(() => {
		settingsForm.registerFormActions(onSubmit, resetForm);
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
						<div class="space-y-3">
							<SwitchWithLabel
								id="localAuthSwitch"
								label={m.security_local_auth_label()}
								description={m.security_local_auth_description()}
								error={$formInputs.authLocalEnabled.error}
								bind:checked={$formInputs.authLocalEnabled.value}
								onCheckedChange={handleLocalSwitchChange}
							/>

							<div class="space-y-2">
								<SwitchWithLabel
									id="oidcAuthSwitch"
									label={m.security_oidc_auth_label()}
									description={data.oidcStatus.envForced
										? m.security_oidc_auth_description_forced()
										: m.security_oidc_auth_description()}
									error={$formInputs.authOidcEnabled.error}
									disabled={data.oidcStatus.envForced}
									bind:checked={$formInputs.authOidcEnabled.value}
									onCheckedChange={handleOidcSwitchChange}
								/>

								{#if isOidcActive()}
									<div class="pl-8 sm:pl-11">
										{#if data.oidcStatus.envForced}
											{#if !data.oidcStatus.envConfigured}
												<Button
													variant="link"
													class="text-destructive h-auto p-0 text-xs hover:underline"
													onclick={openOidcDialog}
												>
													{m.security_server_forces_oidc_missing_env()}
												</Button>
											{:else}
												<Button variant="link" class="h-auto p-0 text-xs text-sky-600 hover:underline" onclick={openOidcDialog}>
													{m.security_oidc_configured_forced_view()}
												</Button>
											{/if}
										{:else}
											<Button variant="link" class="h-auto p-0 text-xs text-sky-600 hover:underline" onclick={openOidcDialog}>
												{m.security_manage_oidc_config()}
											</Button>
										{/if}
									</div>
								{/if}
							</div>

							{#if isOidcActive()}
								<div class="mt-3">
									<SwitchWithLabel
										id="oidcMergeAccountsSwitch"
										label={m.security_oidc_merge_accounts_label()}
										description={m.security_oidc_merge_accounts_description()}
										bind:checked={$formInputs.authOidcMergeAccounts.value}
										onCheckedChange={handleMergeAccountsChange}
									/>
								</div>
							{/if}
						</div>
					</Card.Content>
				</Card.Root>

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
							<Card.Description>Set password strength requirements</Card.Description>
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
		<OidcConfigDialog
			bind:open={showOidcConfigDialog}
			{currentSettings}
			oidcStatus={data.oidcStatus}
			bind:oidcForm={oidcConfigForm}
			onSave={handleSaveOidcConfig}
		/>

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
