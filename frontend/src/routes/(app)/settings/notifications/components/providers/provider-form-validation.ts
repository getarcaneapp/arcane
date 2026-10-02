import type { z } from 'zod/v4';

export type ProviderFieldErrors<T extends object> = Partial<Record<keyof T, string>> & Record<string, string | undefined>;

export function mapZodFieldErrors<T extends object>(validation: z.ZodSafeParseResult<T>): ProviderFieldErrors<T> {
	const errors: Record<string, string | undefined> = {};
	if (validation.success) {
		return errors as ProviderFieldErrors<T>;
	}

	for (const issue of validation.error.issues) {
		const key = issue.path.map(String).join('.');
		if (!key || errors[key]) {
			continue;
		}

		errors[key] = issue.message;
	}

	return errors as ProviderFieldErrors<T>;
}
