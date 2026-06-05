import { m } from '$lib/paraglide/messages';
import { formatDistanceToNow } from 'date-fns';

export function getSwarmSpecName(spec: Record<string, unknown> | null | undefined, fallback: string): string {
	const name = spec && typeof spec === 'object' ? spec['Name'] : undefined;
	return typeof name === 'string' && name.trim() ? name : fallback.slice(0, 12);
}

export function decodeBase64ToText(base64Value: string): string {
	try {
		const binary = atob(base64Value);
		const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
		return new TextDecoder().decode(bytes);
	} catch {
		return '';
	}
}

export function encodeTextToBase64(value: string): string {
	const bytes = new TextEncoder().encode(value);
	let binary = '';
	for (const byte of bytes) {
		binary += String.fromCharCode(byte);
	}
	return btoa(binary);
}

export function formatSwarmTimestamp(timestamp: string): string {
	if (!timestamp) return m.common_unknown();
	return formatDistanceToNow(new Date(timestamp), { addSuffix: true });
}
