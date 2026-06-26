/**
 * Returns a value unique to each call, for use as a cache-busting query param on
 * stream URLs. Without it, intermediaries that key on the URL — a reverse proxy
 * collapsing identical in-flight GETs, the service worker cache, the browser HTTP
 * cache — can collapse multiple tabs' identical stream requests onto a single
 * upstream connection, so only the first tab receives live events.
 *
 * `crypto.randomUUID()` is only defined in secure contexts (HTTPS / localhost),
 * so fall back to a timestamp + random suffix for plain-HTTP deployments.
 */
export function streamCacheBuster(): string {
	if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
		return crypto.randomUUID();
	}
	return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export async function readNdjsonStream(
	body: ReadableStream<Uint8Array>,
	onMessage?: (data: any) => void,
	onLine?: (data: any) => void
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

			let obj: any;
			try {
				obj = JSON.parse(trimmed);
			} catch {
				continue;
			}

			onLine?.(obj);
			onMessage?.(obj);
		}
	}
}
