import type { z } from 'zod/v4';

export function mapZodFieldErrors<T extends object>(validation: z.ZodSafeParseResult<T>): Partial<Record<keyof T, string>> {
	const errors: Partial<Record<keyof T, string>> = {};
	if (validation.success) {
		return errors;
	}

	for (const issue of validation.error.issues) {
		const key = issue.path?.[0] as keyof T | undefined;
		if (!key || errors[key]) {
			continue;
		}

		errors[key] = issue.message;
	}

	return errors;
}
