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
