import { m } from '#lib/paraglide/messages.js';
import { getLocale } from '#lib/paraglide/runtime.js';
import settingsStore from '#lib/stores/config-store.svelte.js';

export interface PasswordPolicy {
	minLength: number;
	requireUppercase: boolean;
	requireLowercase: boolean;
	requireNumber: boolean;
	requireSymbol: boolean;
}

export function parsePasswordPolicyProblem(data: unknown): PasswordPolicy | null {
	if (!data || typeof data !== 'object') return null;
	const record = data as Record<string, unknown>;
	if (record['type'] !== 'urn:arcane:problem:password-policy') return null;
	const details = record['details'];
	if (!details || typeof details !== 'object') return null;
	const policy = (details as Record<string, unknown>)['policy'];
	if (!policy || typeof policy !== 'object') return null;
	const candidate = policy as Record<string, unknown>;
	if (
		typeof candidate['minLength'] !== 'number' ||
		typeof candidate['requireUppercase'] !== 'boolean' ||
		typeof candidate['requireLowercase'] !== 'boolean' ||
		typeof candidate['requireNumber'] !== 'boolean' ||
		typeof candidate['requireSymbol'] !== 'boolean'
	) {
		return null;
	}
	return {
		minLength: candidate['minLength'],
		requireUppercase: candidate['requireUppercase'],
		requireLowercase: candidate['requireLowercase'],
		requireNumber: candidate['requireNumber'],
		requireSymbol: candidate['requireSymbol']
	};
}

export function passwordRequirements(policy?: PasswordPolicy): string {
	const settings = settingsStore.current;
	const length = policy?.minLength ?? settings?.authPasswordMinLength ?? 12;
	const rules = [
		{
			enabled: policy?.requireUppercase ?? settings?.authPasswordRequireUppercase ?? true,
			label: m.password_requirement_uppercase()
		},
		{
			enabled: policy?.requireLowercase ?? settings?.authPasswordRequireLowercase ?? true,
			label: m.password_requirement_lowercase()
		},
		{
			enabled: policy?.requireNumber ?? settings?.authPasswordRequireNumber ?? true,
			label: m.password_requirement_number()
		},
		{
			enabled: policy?.requireSymbol ?? settings?.authPasswordRequireSymbol ?? true,
			label: m.password_requirement_symbol()
		}
	]
		.filter((rule) => rule.enabled)
		.map((rule) => rule.label);

	if (rules.length === 0) return m.password_requirements_min_length({ length });
	return m.password_requirements_with_rules({
		length,
		rules: new Intl.ListFormat(getLocale(), { type: 'conjunction' }).format(rules)
	});
}
