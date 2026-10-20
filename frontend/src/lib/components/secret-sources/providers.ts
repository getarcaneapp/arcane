import { m } from '#lib/paraglide/messages.js';
import type { SecretSourceProvider, SecretSourceSettings } from '#lib/types/secretsource.js';

// Provider metadata shared by the source sheet, the binding form, and the
// summaries. Each provider brings its own target fields component.

const providerOptions: { value: SecretSourceProvider; label: () => string; description: () => string }[] = [
	{
		value: 'infisical',
		label: m.secret_sources_provider_infisical,
		description: m.secret_sources_provider_infisical_description
	}
];

// The choices in alphabetical order by their (translated) name.
export function sortedProviderOptions() {
	return [...providerOptions].sort((a, b) => a.label().localeCompare(b.label()));
}

export function providerLabel(provider: SecretSourceProvider | undefined): string {
	return (providerOptions.find((option) => option.value === provider) ?? providerOptions[0]!).label();
}

// Where a source's credential is sent; the backend asks for the credential
// again when this changes.
export function endpointKey(settings: SecretSourceSettings): string {
	if (settings.infisical) return `infisical|${settings.infisical.siteUrl}`;
	return '';
}
