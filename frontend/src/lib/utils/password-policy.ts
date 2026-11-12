import { z } from 'zod/v4';

import { m } from '#lib/paraglide/messages.js';
import { getLocale } from '#lib/paraglide/runtime.js';
import settingsStore from '#lib/stores/config-store.svelte.js';

const passwordPolicySchema = z.object({
	minLength: z.number(),
	requireUppercase: z.boolean(),
	requireLowercase: z.boolean(),
	requireNumber: z.boolean(),
	requireSymbol: z.boolean()
});

export const passwordPolicyProblemSchema = z.object({
	type: z.literal('urn:arcane:problem:password-policy'),
	details: z.object({ policy: passwordPolicySchema })
});

export function passwordRequirements(policy?: z.infer<typeof passwordPolicySchema>): string {
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
