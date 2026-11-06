import { tryCatch } from '#lib/utils/try-catch.js';

export function streamCacheBuster(): string {
	if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
		return crypto.randomUUID();
	}
	return `${Temporal.Now.instant().epochMilliseconds}-${Math.random().toString(36).slice(2)}`;
}

/**
 * Reads an NDJSON stream, invoking the callbacks per parsed line. A callback
 * returning `true` stops reading and cancels the stream — used by operation
 * streams whose terminal {"done":true} frame marks completion, since proxies
 * do not always propagate the network EOF promptly.
 */
export async function readNdjsonStream<T = unknown>(
	body: ReadableStream<Uint8Array>,
	onMessage?: (data: T) => boolean | void,
	onLine?: (data: T) => boolean | void
): Promise<void> {
	const reader = body.getReader();
	const decoder = new TextDecoder();
	let buffer = '';

	while (true) {
		const { value, done } = await reader.read();
		if (done) break;

		buffer += decoder.decode(value, { stream: true });
		const lines = buffer.split('\n');
		buffer = lines.pop() || '';

		for (const line of lines) {
			const trimmed = line.trim();
			if (!trimmed) continue;

			let obj: T;
			try {
				obj = JSON.parse(trimmed) as T;
			} catch {
				continue;
			}

			const stopFromLine = onLine?.(obj) === true;
			const stopFromMessage = onMessage?.(obj) === true;
			if (stopFromLine || stopFromMessage) {
				await tryCatch(reader.cancel());
				return;
			}
		}
	}
}
