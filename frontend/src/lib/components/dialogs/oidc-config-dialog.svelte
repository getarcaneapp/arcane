<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import KeyIcon from '@lucide/svelte/icons/key';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import InfoIcon from '@lucide/svelte/icons/info';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import { m } from '$lib/paraglide/messages';
	import type { Settings, OidcStatusInfo } from '$lib/types/settings.type';
	import { UseClipboard } from '$lib/hooks/use-clipboard.svelte';

	interface OidcConfig {
		clientId: string;
		clientSecret: string;
		issuerUrl: string;
		scopes: string;
		adminClaim: string;
		adminValue: string;
	}

	interface Props {
		open: boolean;
		currentSettings: Settings;
		oidcStatus: OidcStatusInfo;
		oidcForm: OidcConfig;
		onSave: () => void;
	}

	let { open = $bindable(), currentSettings, oidcStatus, oidcForm = $bindable(), onSave }: Props = $props();

	const DEFAULT_CONFIG: OidcConfig = {
		clientId: '',
		clientSecret: '',
		issuerUrl: '',
		scopes: 'openid email profile',
		adminClaim: '',
		adminValue: ''
	};

	const isViewMode = $derived(oidcStatus.envForced && oidcStatus.envConfigured);
	const isWarningMode = $derived(oidcStatus.envForced && !oidcStatus.envConfigured);
	const redirectUri = $derived(`${globalThis?.location?.origin ?? ''}/auth/oidc/callback`);

	const parsedConfig = $derived.by(() => {
		return {
			clientId: currentSettings.oidcClientId || '',
			clientSecret: '', // Never expose client secret
			issuerUrl: currentSettings.oidcIssuerUrl || '',
			scopes: currentSettings.oidcScopes || 'openid email profile',
			adminClaim: currentSettings.oidcAdminClaim || '',
			adminValue: currentSettings.oidcAdminValue || ''
		} as OidcConfig;
	});

	const dialogTitle = $derived(
		isViewMode
			? m.security_oidc_configured_forced_view()
			: isWarningMode
				? m.security_server_forces_oidc_missing_env()
				: m.security_manage_oidc_config()
	);

	const dialogDescription = $derived(
		isViewMode
			? m.oidc_viewmode_description()
			: isWarningMode
				? m.security_server_forces_oidc_missing_env()
				: m.oidc_configure_description()
	);

	const clipboard = new UseClipboard();

	function handleCopy(text?: string) {
		if (!text) return;
		clipboard.copy(text);
	}

	function handleClose() {
		open = false;
	}
</script>

<ResponsiveDialog bind:open title={dialogTitle} description={dialogDescription} contentClass="sm:max-w-[860px]">
	{#snippet children()}
		<div class="space-y-6">
			{#if isViewMode}
				{@render viewModeContent()}
			{:else}
				{@render editModeContent()}
			{/if}

			{#if isWarningMode}
				{@render warningBanner()}
			{/if}

			{@render redirectUriSection()}
		</div>
	{/snippet}

	{#snippet footer()}
		<Button variant="outline" onclick={handleClose} class="flex-1 sm:flex-none">
			{m.common_close()}
		</Button>
		{#if !isViewMode}
			<Button onclick={onSave} class="flex-1 sm:flex-none">
				<KeyIcon class="mr-2 size-4" />
				{m.common_save()}
			</Button>
		{/if}
	{/snippet}
</ResponsiveDialog>

{#snippet viewModeContent()}
	<div class="bg-card/90 rounded-lg border p-4 backdrop-blur-sm">
		<h3 class="text-base font-semibold">{m.oidc_server_env_config_title()}</h3>
		<p class="text-muted-foreground mt-1 mb-4 text-sm">{m.oidc_server_env_config_description()}</p>

		{#if currentSettings.oidcEnabled}
			<div class="divide-border/60 divide-y text-sm">
				{@render configRow(m.oidc_client_id_label(), parsedConfig.clientId)}
				{@render configRow(m.oidc_issuer_url_label(), parsedConfig.issuerUrl)}
				<div class="flex items-center justify-between py-2">
					<span class="font-medium">{m.oidc_client_secret_label()}</span>
					<span class="text-muted-foreground text-xs italic">{m.oidc_client_secret_hidden()}</span>
				</div>
				{@render configRow(m.oidc_scopes_label(), parsedConfig.scopes || m.oidc_scopes_placeholder())}
				{@render configRow(m.oidc_admin_claim_label(), parsedConfig.adminClaim)}
				{@render configRow(m.oidc_admin_value_label(), parsedConfig.adminValue)}
			</div>
		{:else}
			<div class="py-6 text-center">
				<TriangleAlertIcon class="text-destructive mx-auto mb-2 size-8" />
				<p class="text-destructive text-sm font-medium">{m.oidc_env_config_missing()}</p>
			</div>
		{/if}

		<div class="bg-muted/40 mt-4 rounded-md p-3">
			<p class="text-muted-foreground text-xs">{m.oidc_env_changes_note()}</p>
		</div>
	</div>
{/snippet}

{#snippet configRow(label: string, value: string)}
	<div class="flex items-center justify-between py-2">
		<span class="font-medium">{label}</span>
		<code class="bg-muted rounded px-2 py-1 text-xs">{value || m.common_unknown()}</code>
	</div>
{/snippet}

{#snippet editModeContent()}
	<div class="bg-card/90 rounded-lg border p-4 backdrop-blur-sm">
		<h3 class="text-base font-semibold">{m.oidc_basic_configuration_title()}</h3>
		<p class="text-muted-foreground mt-1 mb-4 text-sm">{m.oidc_basic_description()}</p>

		<div class="space-y-4">
			<div class="space-y-2">
				<Label for="oidcClientId" class="text-sm font-medium">{m.oidc_client_id_label()}</Label>
				<Input
					id="oidcClientId"
					type="text"
					placeholder={m.oidc_client_id_placeholder()}
					bind:value={oidcForm.clientId}
					class="font-mono text-sm"
				/>
			</div>

			<div class="space-y-2">
				<Label for="oidcClientSecret" class="text-sm font-medium">{m.oidc_client_secret_label()}</Label>
				<Input
					id="oidcClientSecret"
					type="password"
					placeholder={m.oidc_client_secret_placeholder()}
					bind:value={oidcForm.clientSecret}
					class="font-mono text-sm"
				/>
			</div>

			<div class="space-y-2">
				<Label for="oidcIssuerUrl" class="text-sm font-medium">{m.oidc_issuer_url_label()}</Label>
				<Input
					id="oidcIssuerUrl"
					type="text"
					placeholder={m.oidc_issuer_url_placeholder()}
					bind:value={oidcForm.issuerUrl}
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
					bind:value={oidcForm.scopes}
					class="font-mono text-sm"
				/>
			</div>

			<div class="pt-2">
				<h4 class="text-sm font-semibold">{m.oidc_admin_role_mapping_title()}</h4>
				<p class="text-muted-foreground mb-3 text-xs">{m.oidc_admin_role_mapping_description()}</p>
				<div class="grid gap-3 sm:grid-cols-2">
					<div class="space-y-2">
						<Label for="oidcAdminClaim" class="text-sm font-medium">{m.oidc_admin_claim_label()}</Label>
						<Input
							id="oidcAdminClaim"
							type="text"
							placeholder={m.oidc_admin_claim_placeholder()}
							bind:value={oidcForm.adminClaim}
							class="font-mono text-sm"
						/>
					</div>
					<div class="space-y-2">
						<Label for="oidcAdminValue" class="text-sm font-medium">{m.oidc_admin_value_label()}</Label>
						<Input
							id="oidcAdminValue"
							type="text"
							placeholder={m.oidc_admin_value_placeholder()}
							bind:value={oidcForm.adminValue}
							class="font-mono text-sm"
						/>
						<p class="text-muted-foreground text-[11px]">{m.oidc_admin_value_help()}</p>
					</div>
				</div>
			</div>
		</div>
	</div>
{/snippet}

{#snippet warningBanner()}
	<div class="border-destructive/50 bg-destructive/5 rounded-lg border p-4">
		<div class="mb-2 flex items-center gap-2">
			<TriangleAlertIcon class="text-destructive size-4" />
			<h3 class="text-destructive text-base font-semibold">
				{m.oidc_server_override_warning_title()}
			</h3>
		</div>
		<p class="text-destructive/80 text-sm">{m.security_server_forces_oidc_missing_env()}</p>
	</div>
{/snippet}

{#snippet redirectUriSection()}
	<div class="bg-card/90 rounded-lg border p-4 backdrop-blur-sm">
		<div class="mb-3 flex items-center gap-2">
			<InfoIcon class="size-4 text-blue-600" />
			<h3 class="text-base font-semibold">{m.oidc_redirect_uri_title()}</h3>
		</div>
		<p class="text-muted-foreground mb-3 text-sm">{m.oidc_redirect_uri_description()}</p>
		<div class="flex items-center gap-2">
			<code class="bg-muted flex-1 rounded p-2 font-mono text-xs break-all">{redirectUri}</code>
			<Button size="sm" variant="outline" onclick={() => handleCopy(redirectUri)} class="shrink-0" title={m.common_copy()}>
				<CopyIcon class="size-3" />
			</Button>
		</div>
	</div>
{/snippet}
