import { m } from '#lib/paraglide/messages.js';
import { getLocale } from '#lib/paraglide/runtime.js';
import settingsStore from '#lib/stores/config-store.svelte.js';

export const PASSWORD_POLICY_PROBLEM_TYPE = 'urn:arcane:problem:password-policy';

export function passwordRequirements(): string {
	const settings = settingsStore.current;
	const length = settings?.authPasswordMinLength ?? 12;
	const rules = [
		{ enabled: settings?.authPasswordRequireUppercase ?? true, label: m.password_requirement_uppercase() },
		{ enabled: settings?.authPasswordRequireLowercase ?? true, label: m.password_requirement_lowercase() },
		{ enabled: settings?.authPasswordRequireNumber ?? true, label: m.password_requirement_number() },
		{ enabled: settings?.authPasswordRequireSymbol ?? true, label: m.password_requirement_symbol() }
	]
		.filter((rule) => rule.enabled)
		.map((rule) => rule.label);

	if (rules.length === 0) return m.password_requirements_min_length({ length });
	return m.password_requirements_with_rules({
		length,
		rules: new Intl.ListFormat(getLocale(), { type: 'conjunction' }).format(rules)
	});
}
