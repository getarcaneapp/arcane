import { m } from '#lib/paraglide/messages.js';
import type { BindingTarget, SecretProvider, SourceSettings } from '#lib/types/secret-source.js';

// Provider metadata shared by the source sheet, the binding forms, and the
// summaries. Infisical and Bitwarden keep their own target components; the
// providers below use ProviderTargetFields.

export const providerOptions: { value: SecretProvider; label: () => string; description: () => string }[] = [
	{
		value: 'infisical',
		label: m.secret_sources_provider_infisical,
		description: m.secret_sources_provider_infisical_description
	},
	{
		value: 'bitwarden',
		label: m.secret_sources_provider_bitwarden,
		description: m.secret_sources_provider_bitwarden_description
	},
	{ value: 'vault', label: m.secret_sources_provider_vault, description: m.secret_sources_provider_vault_description },
	{ value: 'doppler', label: m.secret_sources_provider_doppler, description: m.secret_sources_provider_doppler_description },
	{
		value: 'onepassword',
		label: m.secret_sources_provider_onepassword,
		description: m.secret_sources_provider_onepassword_description
	},
	{
		value: 'protonpass',
		label: m.secret_sources_provider_protonpass,
		description: m.secret_sources_provider_protonpass_description
	},
	{ value: 'http', label: m.secret_sources_provider_http, description: m.secret_sources_provider_http_description }
];

// The choices in alphabetical order by their (translated) name. The generic HTTP
// endpoint is a catch-all rather than a product, so it stays last.
export function sortedProviderOptions() {
	const named = providerOptions.filter((option) => option.value !== 'http');
	named.sort((a, b) => a.label().localeCompare(b.label()));
	return [...named, ...providerOptions.filter((option) => option.value === 'http')];
}

export type GenericProvider = 'vault' | 'doppler' | 'onepassword' | 'protonpass' | 'http';

export function isGenericProvider(provider: SecretProvider | undefined): provider is GenericProvider {
	return (
		provider === 'vault' ||
		provider === 'doppler' ||
		provider === 'onepassword' ||
		provider === 'protonpass' ||
		provider === 'http'
	);
}

export function providerLabel(provider: SecretProvider | undefined): string {
	return (providerOptions.find((option) => option.value === provider) ?? providerOptions[0]!).label();
}

// Providers whose guided setup can write secrets.
export function providerCanSetUp(provider: SecretProvider): boolean {
	return provider === 'infisical' || provider === 'bitwarden' || provider === 'vault';
}

// Fills in the target of a generic provider so its fields have values to bind.
export function withDefaultTarget(provider: GenericProvider, target: BindingTarget): BindingTarget {
	switch (provider) {
		case 'vault':
			return { vault: target.vault ?? { mount: 'secret', path: '', kvVersion: 2 } };
		case 'doppler':
			return { doppler: target.doppler ?? { project: '', config: '' } };
		case 'onepassword':
			return { onepassword: target.onepassword ?? { scope: 'vault', vaultId: '', itemId: '', name: '' } };
		case 'protonpass':
			return { protonpass: target.protonpass ?? { scope: 'vault', vaultId: '', itemId: '', name: '' } };
		case 'http':
			return { http: target.http ?? { path: '' } };
	}
}

export function isGenericTargetComplete(provider: GenericProvider, target: BindingTarget): boolean {
	switch (provider) {
		case 'vault': {
			// A path ending in "/" is a folder, not a secret.
			const path = target.vault?.path.trim() ?? '';
			return !!path && !path.endsWith('/');
		}
		case 'doppler': {
			// {} is a service token's own config; otherwise both must be picked.
			const doppler = target.doppler;
			if (!doppler) return false;
			if (doppler.project === undefined && doppler.config === undefined) return true;
			return !!doppler.project?.trim() && !!doppler.config?.trim();
		}
		case 'onepassword': {
			const op = target.onepassword;
			return !!op?.vaultId && (op.scope === 'vault' || !!op.itemId);
		}
		case 'protonpass': {
			const pp = target.protonpass;
			return !!pp?.vaultId && (pp.scope === 'vault' || !!pp.itemId);
		}
		case 'http':
			return !!target.http;
	}
}

// Keeps only the selected provider's target, as the API expects.
export function pickGenericTarget(provider: GenericProvider, target: BindingTarget): BindingTarget {
	return withDefaultTarget(provider, target);
}

// Where a source's credential is sent; the backend asks for the credential
// again when this changes.
export function endpointKey(settings: SourceSettings): string {
	if (settings.infisical) return `infisical|${settings.infisical.siteUrl}`;
	if (settings.vault) {
		const vault = settings.vault;
		return `vault|${vault.address}|${vault.authMethod}|${vault.roleId ?? ''}|${vault.appRoleMount ?? ''}`;
	}
	if (settings.doppler) return `doppler|${settings.doppler.apiUrl ?? ''}`;
	if (settings.onepassword) return `onepassword|${settings.onepassword.serverUrl}`;
	if (settings.protonpass) return `protonpass|${settings.protonpass.kitUrl}`;
	if (settings.http) return `http|${settings.http.baseUrl}`;
	return '';
}

export function describeGenericTarget(target: BindingTarget): string {
	if (target.vault) {
		return `${target.vault.mount}/${target.vault.path} · KV v${target.vault.kvVersion}`;
	}
	if (target.doppler) {
		return target.doppler.project ? `${target.doppler.project} · ${target.doppler.config}` : m.doppler_service_token_config();
	}
	if (target.onepassword) {
		const scope = target.onepassword.scope === 'item' ? m.onepassword_scope_item() : m.onepassword_scope_vault();
		return `${scope} · ${target.onepassword.name || target.onepassword.itemId || target.onepassword.vaultId}`;
	}
	if (target.protonpass) {
		const scope = target.protonpass.scope === 'item' ? m.onepassword_scope_item() : m.onepassword_scope_vault();
		return `${scope} · ${target.protonpass.name || target.protonpass.itemId || target.protonpass.vaultId}`;
	}
	if (target.http) {
		return target.http.path ? `/${target.http.path}` : m.http_target_root();
	}
	return '';
}
