<script lang="ts">
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';

	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { InfoIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { Settings } from '#lib/types/settings.js';
	import { createSettingsForm } from '#lib/utils/settings-form.js';

	import type { PageProps } from './$types';
	import FederatedCredentialsTab from './components/federated-credentials-tab.svelte';
	import GeneralSettings from './components/general-settings.svelte';
	import LdapSettings from './components/ldap-settings.svelte';
	import OidcSettings from './components/oidc-settings.svelte';
	import RoleMappingsTab from './components/role-mappings-tab.svelte';

	let { data }: PageProps = $props();
	type AuthenticationTab = 'general' | 'oidc' | 'ldap' | 'mappings' | 'federated';
	const formTabs: AuthenticationTab[] = ['general', 'oidc', 'ldap'];

	const authenticationTabItems: TabItem[] = $derived([
		{ value: 'general', label: m.common_general() },
		{ value: 'oidc', label: m.common_oidc() },
		{ value: 'ldap', label: m.ldap() },
		{ value: 'mappings', label: m.oidc_role_mappings_title() },
		{ value: 'federated', label: m.federated_credential_page_title() }
	]);
	const urlTab = useUrlTab<AuthenticationTab>({
		validTabs: () => authenticationTabItems.map((tab) => tab.value as AuthenticationTab),
		defaultTab: () => 'general'
	});
	const activeTab = $derived(urlTab.value);
	const isFormTab = $derived(formTabs.includes(activeTab));

	const currentSettings = $derived<Settings>(settingsStore.current || data.settings!);
	const isReadOnly = $derived(settingsStore.readOnly);
	const isOidcLocked = $derived(settingsStore.isLocked('oidcEnabled'));
	const isLdapLocked = $derived(settingsStore.isLocked('ldapEnabled'));
	const isAutoLoginEnabled = $derived(settingsStore.autoLoginEnabled.current);

	const formSchema = z
		.object({
			authLocalEnabled: z.boolean(),
			authSessionTimeout: z.coerce
				.number()
				.int(m.security_session_timeout_integer())
				.min(15, m.security_session_timeout_min())
				.max(525600, m.security_session_timeout_max()),
			authPasswordMinLength: z.coerce
				.number()
				.int(m.minimum_length_range())
				.min(8, m.minimum_length_range())
				.max(128, m.minimum_length_range()),
			authPasswordRequireUppercase: z.boolean(),
			authPasswordRequireLowercase: z.boolean(),
			authPasswordRequireNumber: z.boolean(),
			authPasswordRequireSymbol: z.boolean(),
			hstsEnabled: z.boolean(),
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
			oidcProviderLogoUrl: z.string(),
			ldapEnabled: z.boolean(),
			ldapUrl: z.string(),
			ldapBindDn: z.string(),
			ldapBindPassword: z.string(),
			ldapClearBindPassword: z.boolean(),
			ldapBase: z.string(),
			ldapUserSearchFilter: z.string(),
			ldapUserGroupSearchFilter: z.string(),
			ldapSkipCertVerify: z.boolean(),
			ldapAttributeUserUniqueIdentifier: z.string(),
			ldapAttributeUserUsername: z.string(),
			ldapAttributeUserEmail: z.string(),
			ldapAttributeUserDisplayName: z.string(),
			ldapAttributeUserProfilePicture: z.string(),
			ldapAttributeGroupMember: z.string(),
			ldapAttributeGroupName: z.string(),
			ldapSoftDeleteUsers: z.boolean(),
			ldapSyncInterval: z.string()
		})
		.superRefine((formData, ctx) => {
			const oidcEnabledForAuthValidation = isOidcLocked ? currentSettings.oidcEnabled : formData.oidcEnabled;
			const ldapEnabledForAuthValidation = isLdapLocked ? currentSettings.ldapEnabled : formData.ldapEnabled;
			if (!oidcEnabledForAuthValidation && !ldapEnabledForAuthValidation && !formData.authLocalEnabled) {
				ctx.addIssue({
					code: 'custom',
					message: m.security_enable_one_provider(),
					path: ['authLocalEnabled']
				});
			}
			if (formData.oidcEnabled && !isOidcLocked) {
				for (const field of ['oidcClientId', 'oidcIssuerUrl'] as const) {
					if (!formData[field].trim()) {
						ctx.addIssue({ code: 'custom', message: m.security_oidc_required_fields(), path: [field] });
					}
				}
				if ((!currentSettings.oidcClientSecret || formData.oidcClearClientSecret) && !formData.oidcClientSecret.trim()) {
					ctx.addIssue({ code: 'custom', message: m.security_oidc_required_fields(), path: ['oidcClientSecret'] });
				}
			}
			if (formData.ldapEnabled && !isLdapLocked) {
				for (const field of [
					'ldapUrl',
					'ldapBindDn',
					'ldapBase',
					'ldapAttributeUserUniqueIdentifier',
					'ldapAttributeUserUsername',
					'ldapSyncInterval'
				] as const) {
					if (!formData[field].trim()) {
						ctx.addIssue({ code: 'custom', message: m.required_when_ldap_is_enabled(), path: [field] });
					}
				}
				if ((!currentSettings.ldapBindPassword || formData.ldapClearBindPassword) && !formData.ldapBindPassword.trim()) {
					ctx.addIssue({ code: 'custom', message: m.required_when_ldap_is_enabled(), path: ['ldapBindPassword'] });
				}
			}
		});

	function readFormValues() {
		const source = settingsStore.current || data.settings!;
		return {
			authLocalEnabled: source.authLocalEnabled,
			authSessionTimeout: source.authSessionTimeout,
			authPasswordMinLength: source.authPasswordMinLength,
			authPasswordRequireUppercase: source.authPasswordRequireUppercase,
			authPasswordRequireLowercase: source.authPasswordRequireLowercase,
			authPasswordRequireNumber: source.authPasswordRequireNumber,
			authPasswordRequireSymbol: source.authPasswordRequireSymbol,
			hstsEnabled: source.hstsEnabled,
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
			oidcProviderLogoUrl: source.oidcProviderLogoUrl,
			ldapEnabled: source.ldapEnabled,
			ldapUrl: source.ldapUrl,
			ldapBindDn: source.ldapBindDn,
			ldapBindPassword: '',
			ldapClearBindPassword: false,
			ldapBase: source.ldapBase,
			ldapUserSearchFilter: source.ldapUserSearchFilter,
			ldapUserGroupSearchFilter: source.ldapUserGroupSearchFilter,
			ldapSkipCertVerify: source.ldapSkipCertVerify,
			ldapAttributeUserUniqueIdentifier: source.ldapAttributeUserUniqueIdentifier,
			ldapAttributeUserUsername: source.ldapAttributeUserUsername,
			ldapAttributeUserEmail: source.ldapAttributeUserEmail,
			ldapAttributeUserDisplayName: source.ldapAttributeUserDisplayName,
			ldapAttributeUserProfilePicture: source.ldapAttributeUserProfilePicture,
			ldapAttributeGroupMember: source.ldapAttributeGroupMember,
			ldapAttributeGroupName: source.ldapAttributeGroupName,
			ldapSoftDeleteUsers: source.ldapSoftDeleteUsers,
			ldapSyncInterval: source.ldapSyncInterval
		};
	}

	function tabForField(key: string): AuthenticationTab {
		if (key.startsWith('oidc')) return 'oidc';
		if (key.startsWith('ldap')) return 'ldap';
		return 'general';
	}

	function showFirstInvalidTab() {
		const invalidTabs = Object.entries(formInputs)
			.filter(([, input]) => input.error)
			.map(([key]) => tabForField(key));
		if (invalidTabs.includes(activeTab)) return;
		const target = formTabs.find((tab) => invalidTabs.includes(tab));
		if (target) urlTab.select(target);
	}

	const { formInputs } = untrack(() =>
		createSettingsForm({
			schema: formSchema,
			currentSettings: readFormValues(),
			getCurrentSettings: readFormValues,
			onSave: async ({ oidcClientSecret, oidcClearClientSecret, ldapBindPassword, ldapClearBindPassword, ...rest }) => {
				const payload: Partial<Settings> = rest;
				if (oidcClearClientSecret) payload.oidcClientSecret = '';
				else if (oidcClientSecret) payload.oidcClientSecret = oidcClientSecret;
				if (ldapClearBindPassword) payload.ldapBindPassword = '';
				else if (ldapBindPassword) payload.ldapBindPassword = ldapBindPassword;
				await settingsStore.save(payload);
			},
			onSuccess: () => {
				formInputs.oidcClientSecret.value = '';
				formInputs.oidcClearClientSecret.value = false;
				formInputs.ldapBindPassword.value = '';
				formInputs.ldapClearBindPassword.value = false;
			},
			onInvalid: showFirstInvalidTab,
			successMessage: m.security_settings_saved(),
			errorMessage: m.security_settings_save_failed()
		})
	);

	const isOidcEnabledForAuthValidation = $derived.by(() =>
		isOidcLocked ? currentSettings.oidcEnabled : formInputs.oidcEnabled.value
	);
	const isLdapEnabledForAuthValidation = $derived.by(() =>
		isLdapLocked ? currentSettings.ldapEnabled : formInputs.ldapEnabled.value
	);
	const showLdapDetails = $derived(formInputs.ldapEnabled.value || (isLdapLocked && currentSettings.ldapEnabled));
	const showOidcDetails = $derived(formInputs.oidcEnabled.value || (isOidcLocked && currentSettings.oidcEnabled));

	function handleLocalSwitchChange(checked: boolean) {
		if (!checked && !isOidcEnabledForAuthValidation && !isLdapEnabledForAuthValidation) {
			formInputs.authLocalEnabled.value = true;
			toast.error(m.security_enable_one_provider_error());
			return;
		}
		formInputs.authLocalEnabled.value = checked;
	}

	function handleOidcEnabledChange(checked: boolean) {
		if (!checked && !formInputs.authLocalEnabled.value && !isOidcLocked && !isLdapEnabledForAuthValidation) {
			formInputs.authLocalEnabled.value = true;
			toast.info(m.security_local_enabled_info());
		}
		formInputs.oidcEnabled.value = checked;
	}

	function handleLdapEnabledChange(checked: boolean) {
		if (!checked && !formInputs.authLocalEnabled.value && !isLdapLocked && !isOidcEnabledForAuthValidation) {
			formInputs.authLocalEnabled.value = true;
			toast.info(m.security_local_enabled_info());
		}
		formInputs.ldapEnabled.value = checked;
	}
</script>

{#snippet autoLoginAlert()}
	<Alert.Root variant="warning-subtle">
		<InfoIcon class="h-4 w-4 text-warning" />
		<Alert.Title>{m.security_auto_login_enabled_title()}</Alert.Title>
		<Alert.Description>{m.security_auto_login_enabled_description()}</Alert.Description>
	</Alert.Root>
{/snippet}

<SettingsPageLayout
	title={m.authentication()}
	description={m.authentication_description()}
	pageType={isFormTab ? 'form' : 'management'}
	showReadOnlyTag={isFormTab && isReadOnly}
>
	{#snippet mainContent()}
		<Tabs.Root value={activeTab}>
			<TabBar items={authenticationTabItems} onValueChange={urlTab.select} />

			<Tabs.Content value="general" class="mt-6">
				<fieldset disabled={isReadOnly} class="relative space-y-8">
					{#if isAutoLoginEnabled}
						{@render autoLoginAlert()}
					{/if}
					<GeneralSettings {formInputs} showLocalAuth={!isAutoLoginEnabled} onLocalAuthChange={handleLocalSwitchChange} />
				</fieldset>
			</Tabs.Content>

			<Tabs.Content value="oidc" class="mt-6">
				{#if isAutoLoginEnabled}
					{@render autoLoginAlert()}
				{:else}
					<fieldset disabled={isReadOnly} class="relative space-y-8">
						<SettingsSection title={m.common_oidc()}>
							<SettingsRow
								for="oidcEnabledSwitch"
								label={m.security_oidc_auth_label()}
								description={m.security_oidc_auth_description()}
								layout="switch"
								lock={settingsStore.lock('oidcEnabled')}
							>
								{#snippet children({ describedBy })}
									<Switch
										id="oidcEnabledSwitch"
										disabled={isOidcLocked}
										bind:checked={formInputs.oidcEnabled.value}
										onCheckedChange={handleOidcEnabledChange}
										aria-describedby={describedBy}
									/>
								{/snippet}
							</SettingsRow>
						</SettingsSection>

						{#if showOidcDetails}
							<OidcSettings
								{formInputs}
								hasStoredClientSecret={!!currentSettings.oidcClientSecret}
								storedMergeAccounts={currentSettings.oidcMergeAccounts}
							/>
						{/if}
					</fieldset>
				{/if}
			</Tabs.Content>

			<Tabs.Content value="ldap" class="mt-6">
				{#if isAutoLoginEnabled}
					{@render autoLoginAlert()}
				{:else}
					<fieldset disabled={isReadOnly} class="relative space-y-8">
						<SettingsSection title={m.ldap()}>
							<SettingsRow
								for="ldapEnabledSwitch"
								label={m.ldap_authentication()}
								description={m.ldap_authentication_description()}
								layout="switch"
								lock={settingsStore.lock('ldapEnabled')}
							>
								{#snippet children({ describedBy })}
									<Switch
										id="ldapEnabledSwitch"
										disabled={isLdapLocked}
										bind:checked={formInputs.ldapEnabled.value}
										onCheckedChange={handleLdapEnabledChange}
										aria-describedby={describedBy}
									/>
								{/snippet}
							</SettingsRow>
						</SettingsSection>

						{#if showLdapDetails}
							<LdapSettings
								{formInputs}
								hasStoredBindPassword={!!currentSettings.ldapBindPassword}
								canSync={currentSettings.ldapEnabled}
							/>
						{/if}
					</fieldset>
				{/if}
			</Tabs.Content>

			<Tabs.Content value="mappings" class="mt-6">
				{#if isAutoLoginEnabled}
					{@render autoLoginAlert()}
				{:else}
					<RoleMappingsTab initialMappings={data.oidcMappings} roles={data.roles} environments={data.environments} />
				{/if}
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
</SettingsPageLayout>
