import type { Result } from '#lib/types/result.js';

export async function tryCatch<T, E = Error>(promise: Promise<T>): Promise<Result<T, E>> {
	try {
		const data = await promise;
		return { data, error: null };
	} catch (error) {
		return { data: null, error: error as E };
	}
}

export function trySync<T, E = Error>(fn: () => T): Result<T, E> {
	try {
		return { data: fn(), error: null };
	} catch (error) {
		return { data: null, error: error as E };
	}
}
