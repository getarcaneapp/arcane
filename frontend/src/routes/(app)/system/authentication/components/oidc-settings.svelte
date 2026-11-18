<script lang="ts" module>
	import type { Settings } from '#lib/types/settings.js';

	export type OidcSettingsFormValues = Pick<
		Settings,
		| 'oidcClientId'
		| 'oidcIssuerUrl'
		| 'oidcScopes'
		| 'oidcGroupsClaim'
		| 'oidcProviderName'
		| 'oidcProviderLogoUrl'
		| 'oidcMergeAccounts'
		| 'oidcSkipTlsVerify'
		| 'oidcAutoRedirectToProvider'
	> & {
		oidcClientSecret: string;
		oidcClearClientSecret: boolean;
	};
</script>

<script lang="ts">
	import * as AlertDialog from '@arcane/ui/alert-dialog';
	import { Input } from '@arcane/ui/input';
	import { Switch } from '@arcane/ui/switch';

	import { CopyButton } from '#lib/components/copy-button/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { FormInputs } from '#lib/types/ui/form.js';

	type TextFieldKey =
		| 'oidcClientId'
		| 'oidcIssuerUrl'
		| 'oidcProviderName'
		| 'oidcProviderLogoUrl'
		| 'oidcScopes'
		| 'oidcGroupsClaim';

	type TextField = {
		key: TextFieldKey;
		label: string;
		description?: string;
		helpText?: string;
		placeholder: string;
	};

	let {
		formInputs,
		hasStoredClientSecret,
		storedMergeAccounts
	}: {
		formInputs: FormInputs<OidcSettingsFormValues>;
		hasStoredClientSecret: boolean;
		storedMergeAccounts: boolean;
	} = $props();

	let showMergeAccountsAlert = $state(false);

	const redirectUri = $derived(`${globalThis?.location?.origin ?? ''}/auth/oidc/callback`);

	const clientFields: TextField[] = [
		{
			key: 'oidcClientId',
			label: m.oidc_client_id_label(),
			placeholder: m.oidc_client_id_placeholder()
		}
	];

	const providerFields: TextField[] = [
		{
			key: 'oidcIssuerUrl',
			label: m.oidc_issuer_url_label(),
			description: m.oidc_issuer_url_description(),
			placeholder: m.oidc_issuer_url_placeholder()
		},
		{
			key: 'oidcProviderName',
			label: m.oidc_provider_name_label(),
			description: m.oidc_provider_name_description(),
			placeholder: m.oidc_provider_name_placeholder()
		},
		{
			key: 'oidcProviderLogoUrl',
			label: m.oidc_provider_logo_url_label(),
			description: m.oidc_provider_logo_url_description(),
			placeholder: m.oidc_provider_logo_url_placeholder()
		},
		{
			key: 'oidcScopes',
			label: m.oidc_scopes_label(),
			placeholder: m.oidc_scopes_placeholder()
		},
		{
			key: 'oidcGroupsClaim',
			label: m.oidc_groups_claim_label(),
			helpText: m.oidc_groups_claim_help(),
			placeholder: m.oidc_groups_claim_placeholder()
		}
	];

	function handleMergeAccountsChange(checked: boolean) {
		if (checked && !storedMergeAccounts) {
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

{#snippet textRows(fields: TextField[])}
	{#each fields as field (field.key)}
		<SettingsRow
			for={field.key}
			label={field.label}
			description={field.description}
			helpText={field.helpText}
			error={formInputs[field.key].error}
			lock={settingsStore.lock(field.key)}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id={field.key}
					placeholder={field.placeholder}
					bind:value={formInputs[field.key].value}
					aria-invalid={!!formInputs[field.key].error}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
		</SettingsRow>
	{/each}
{/snippet}

<SettingsSection title={m.common_configuration()}>
	{@render textRows(clientFields)}
	<SettingsRow
		for="oidcClientSecret"
		label={m.oidc_client_secret_label()}
		helpText={m.security_oidc_client_secret_help()}
		error={formInputs.oidcClientSecret.error}
		lock={settingsStore.lock('oidcClientSecret')}
	>
		{#snippet children({ describedBy, errorId })}
			<Input
				id="oidcClientSecret"
				type="password"
				placeholder={m.oidc_client_secret_placeholder()}
				disabled={formInputs.oidcClearClientSecret.value}
				bind:value={formInputs.oidcClientSecret.value}
				aria-invalid={!!formInputs.oidcClientSecret.error}
				aria-describedby={describedBy}
				aria-errormessage={errorId}
			/>
		{/snippet}
	</SettingsRow>
	{#if hasStoredClientSecret && !settingsStore.isLocked('oidcClientSecret')}
		<SettingsRow for="clearOidcClientSecret" label={m.security_oidc_clear_client_secret()} layout="switch">
			{#snippet children({ describedBy })}
				<Switch id="clearOidcClientSecret" bind:checked={formInputs.oidcClearClientSecret.value} aria-describedby={describedBy} />
			{/snippet}
		</SettingsRow>
	{/if}
	{@render textRows(providerFields)}
	<SettingsRow
		for="oidcMergeAccountsSwitch"
		label={m.security_oidc_merge_accounts_label()}
		description={m.security_oidc_merge_accounts_description()}
		layout="switch"
		lock={settingsStore.lock('oidcMergeAccounts')}
	>
		{#snippet children({ describedBy })}
			<Switch
				id="oidcMergeAccountsSwitch"
				bind:checked={formInputs.oidcMergeAccounts.value}
				onCheckedChange={handleMergeAccountsChange}
				aria-describedby={describedBy}
			/>
		{/snippet}
	</SettingsRow>
	<SettingsRow
		for="oidcSkipTlsVerifySwitch"
		label={m.oidc_skip_tls_verify_label()}
		description={m.oidc_skip_tls_verify_description()}
		layout="switch"
		lock={settingsStore.lock('oidcSkipTlsVerify')}
	>
		{#snippet children({ describedBy })}
			<Switch id="oidcSkipTlsVerifySwitch" bind:checked={formInputs.oidcSkipTlsVerify.value} aria-describedby={describedBy} />
		{/snippet}
	</SettingsRow>
	<SettingsRow
		for="oidcAutoRedirectSwitch"
		label={m.oidc_auto_redirect_label()}
		description={m.oidc_auto_redirect_description()}
		layout="switch"
		lock={settingsStore.lock('oidcAutoRedirectToProvider')}
	>
		{#snippet children({ describedBy })}
			<Switch
				id="oidcAutoRedirectSwitch"
				bind:checked={formInputs.oidcAutoRedirectToProvider.value}
				aria-describedby={describedBy}
			/>
		{/snippet}
	</SettingsRow>
	<SettingsRow label={m.oidc_redirect_uri_title()} description={m.oidc_redirect_uri_description()} layout="wide">
		<div class="flex items-center gap-2">
			<code class="flex-1 rounded-md bg-muted/60 p-2 font-mono text-xs break-all">{redirectUri}</code>
			<CopyButton text={redirectUri} size="sm" variant="outline" class="shrink-0" title={m.common_copy()} />
		</div>
	</SettingsRow>
</SettingsSection>

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
